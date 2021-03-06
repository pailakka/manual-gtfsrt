package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"

	"./gtfsrtproto"
)

type ExternalFeed struct {
	URL url.URL
	Msg gtfsrtproto.FeedMessage
}

var currentFeedMessage *gtfsrtproto.FeedMessage
var currentFeedJSON string
var archiveFolder string
var mergeFeeds []*ExternalFeed

func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Nykyinen GTFS-RT lähde:\n%s\nNykyinen tuotettu GTFS-RT:\n%s", currentFeedJSON, proto.MarshalTextString(currentFeedMessage))
}

func mergedIndexHandler(w http.ResponseWriter, r *http.Request) {
	feedmsg := getMergedFeedMessage()
	fmt.Fprintf(w, "Nykyinen yhdistetty GTFS-RT:\n%s", proto.MarshalTextString(&feedmsg))
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	username := os.Getenv("GTFSRT_EDIT_USER")
	password := os.Getenv("GTFSRT_EDIT_PASS")

	user, pass, ok := r.BasicAuth()

	if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="Manual GTFS-RT feed"`)
		w.WriteHeader(401)
		w.Write([]byte("Unauthorised.\n"))
		return
	}

	switch r.Method {
	case "POST":
		err := r.ParseForm()

		if err != nil {
			log.Panic(err)
		}

		msgJSON := r.PostFormValue("msg")

		var fm gtfsrtproto.FeedMessage

		err = jsonpb.UnmarshalString(msgJSON, &fm)

		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		currentFeedJSON = msgJSON

		currentFeedMessage = &fm

		feedHead := currentFeedMessage.GetHeader()
		nowUnix := uint64(time.Now().Unix())
		feedHead.Timestamp = proto.Uint64(nowUnix)

		if len(archiveFolder) > 0 {
			err = ioutil.WriteFile(path.Join(archiveFolder, fmt.Sprintf("feedmessage_%s.json", time.Now().Format("20060102150405"))), []byte(currentFeedJSON), 0664)
			err = ioutil.WriteFile(path.Join(archiveFolder, "feedmessage_latest.json"), []byte(currentFeedJSON), 0664)

			if err != nil {
				log.Panic(err)
			}
		}
		log.Print("FeedMessage updated")
	}

	w.Header().Set("Content-Type", "text/html")
	msgJSON, err := json.Marshal(currentFeedMessage)

	if err != nil {
		log.Panic(err)
	}

	var msgJSONFormatted bytes.Buffer

	err = json.Indent(&msgJSONFormatted, msgJSON, "", "    ")

	if err != nil {
		log.Panic(err)
	}

	fmt.Fprintf(w, "<html><body><form method=\"post\" action=\"\"><textarea cols=\"200\" rows=\"50\" name=\"msg\">%s</textarea><br/><input type=\"submit\" name=\"s\" value=\"Aseta viesti\"/></form></body></html>", msgJSONFormatted.Bytes())

	return

}

func filterEntitiesByType(retainType string, entities []*gtfsrtproto.FeedEntity) (updateEntities []*gtfsrtproto.FeedEntity) {
	for _, e := range entities {
		retain := false
		switch retainType {
		case "alert":
			if e.Alert != nil {
				retain = true
			}
		case "update":
			if e.TripUpdate != nil {
				retain = true
			}
		case "vehicle":
			if e.Vehicle != nil {
				retain = true
			}
		}

		if retain {
			updateEntities = append(updateEntities, e)
		}

	}

	return updateEntities
}

func getMergedFeedMessage() gtfsrtproto.FeedMessage {
	feedmsg := *currentFeedMessage
	for _, ef := range mergeFeeds {
		proto.Merge(&feedmsg, &ef.Msg)
	}

	return feedmsg
}
func gtfsrtAlertsHandler(w http.ResponseWriter, r *http.Request) {
	feedmsg := getMergedFeedMessage()
	feedmsg.Entity = filterEntitiesByType("alert", feedmsg.Entity)

	pbbytes, err := proto.Marshal(&feedmsg)

	if err != nil {
		log.Panic(err)
	}
	w.Write(pbbytes)
}

func gtfsrtUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	feedmsg := getMergedFeedMessage()
	feedmsg.Entity = filterEntitiesByType("update", feedmsg.Entity)

	pbbytes, err := proto.Marshal(&feedmsg)

	if err != nil {
		log.Panic(err)
	}
	w.Write(pbbytes)
}

func gtfsrtVehiclesHandler(w http.ResponseWriter, r *http.Request) {
	feedmsg := getMergedFeedMessage()
	feedmsg.Entity = filterEntitiesByType("vehicle", feedmsg.Entity)

	pbbytes, err := proto.Marshal(&feedmsg)

	if err != nil {
		log.Panic(err)
	}
	w.Write(pbbytes)
}

func updateExternalFeed(ef *ExternalFeed) {
	client := &http.Client{}
	resp, err := client.Get((*ef).URL.String())

	if err != nil {
		log.Print(err)
		return
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Print(err)
		return
	}

	p := gtfsrtproto.FeedMessage{}
	proto.Unmarshal(data, &p)
	(*ef).Msg = p

	log.Printf("External feed updated: %s", (*ef).URL.String())
}

func main() {
	currentFeedMessage = &gtfsrtproto.FeedMessage{}

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/merged", mergedIndexHandler)
	http.HandleFunc("/edit", editHandler)
	http.HandleFunc("/gtfsrt/alerts", gtfsrtAlertsHandler)
	http.HandleFunc("/gtfsrt/updates", gtfsrtUpdatesHandler)
	http.HandleFunc("/gtfsrt/vehicles", gtfsrtVehiclesHandler)

	spew.Dump(os.Getenv("MERGE_FEEDS"))
	mergeFeedsRaw := strings.Split(os.Getenv("MERGE_FEEDS"), ";")

	for _, mf := range mergeFeedsRaw {
		if len(mf) == 0 {
			continue
		}
		u, err := url.Parse(mf)
		if err != nil {
			log.Printf("Merge feed url skipped: %s", mf)
		}
		log.Printf("Merge feed url loaded: %s", u.String())

		ef := ExternalFeed{URL: *u, Msg: gtfsrtproto.FeedMessage{}}
		go func(f *ExternalFeed) {
			ticker := time.NewTicker(60 * time.Second)
			go updateExternalFeed(f)
			for {
				select {
				case <-ticker.C:
					go updateExternalFeed(f)
				}
			}
		}(&ef)
		mergeFeeds = append(mergeFeeds, &ef)
	}

	port := os.Getenv("PORT")
	if len(port) == 0 {
		port = "8080"
	}

	archiveFolder = os.Getenv("ARCHIVE_FOLDER")

	if len(archiveFolder) > 0 {

		_, err := os.Stat(archiveFolder)
		if os.IsNotExist(err) {
			archiveFolder = ""
			log.Print("Archive folder does not exists")

		}

		_, err = os.Stat(path.Join(archiveFolder, "feedmessage_latest.json"))
		if !os.IsNotExist(err) {
			jsondata, err := ioutil.ReadFile(path.Join(archiveFolder, "feedmessage_latest.json"))
			if err != nil {
				log.Panic(err)
			}
			currentFeedJSON = string(jsondata[:len(jsondata)])
			err = jsonpb.UnmarshalString(currentFeedJSON, currentFeedMessage)
			if err != nil {
				log.Panic(err)
			}

		}
	}

	log.Print("Manual GTFS-RT server started")
	http.ListenAndServe(":"+port, nil)

}
