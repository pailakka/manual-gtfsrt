package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/golang/protobuf/proto"

	"./gtfsrtproto"
)

var currentFeedMessage *gtfsrtproto.FeedMessage
var currentFeedJSON []byte
var archiveFolder string

func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Nykyinen GTFS-RT lähde:\n%s\nNykyinen tuotettu GTFS-RT:\n%s", currentFeedJSON, proto.MarshalTextString(currentFeedMessage))
}

func editHandler(w http.ResponseWriter, r *http.Request) {

	switch r.Method {
	case "GET":
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

		fmt.Fprintf(w, "<html><body><form method=\"post\" action=\"/edit\"><textarea cols=\"200\" rows=\"50\" name=\"msg\">%s</textarea><br/><input type=\"submit\" name=\"s\" value=\"Aseta viesti\"/></form></body></html>", msgJSONFormatted.Bytes())

		return
	case "POST":
		err := r.ParseForm()

		if err != nil {
			log.Panic(err)
		}

		msgJSON := []byte(r.PostFormValue("msg"))

		var fm gtfsrtproto.FeedMessage

		err = json.Unmarshal(msgJSON, &fm)

		if err != nil {
			log.Panic(err)
		}

		currentFeedJSON = msgJSON
		currentFeedMessage = &fm

		if len(archiveFolder) > 0 {
			err = ioutil.WriteFile(path.Join(archiveFolder, fmt.Sprintf("feedmessage_%s.json", time.Now().Format("20060102150405"))), currentFeedJSON, 0664)

			if err != nil {
				log.Panic(err)
			}
		}

		http.Redirect(w, r, "/", 301)
		return
	}

}

func gtfsrtHandler(w http.ResponseWriter, r *http.Request) {

	pbbytes, err := proto.Marshal(currentFeedMessage)

	if err != nil {
		log.Panic(err)
	}
	w.Write(pbbytes)
}

func main() {
	currentFeedMessage = &gtfsrtproto.FeedMessage{}

	jsondata, err := ioutil.ReadFile("sample_feed.json")
	if err != nil {
		log.Panic(err)
	}
	currentFeedJSON = jsondata
	err = json.Unmarshal(currentFeedJSON, &currentFeedMessage)

	if err != nil {
		log.Panic(err)
	}

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/edit", editHandler)
	http.HandleFunc("/gtfsrt", gtfsrtHandler)

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
	}
	http.ListenAndServe(":"+port, nil)

}
