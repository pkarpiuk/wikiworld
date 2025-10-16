package events

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	sse "github.com/r3labs/sse"

	utils "wikidumptools/utils"
)

/*
Warunki, jakie musi spełniać event pobrany z kanału:
  type: edit|new
  namespace: 0
  bot: false
ACHTUNG: program kończy działanie gdy następuje zmiana daty (UTC)
*/

type OldNew struct {
	Old int64 `json:"old"`
	New int64 `json:"new"`
}

type Event struct {
	Type      string  `json:"type"`
	Wiki      string  `json:"wiki"`
	Title     string  `json:"title"`
	Comment   string  `json:"comment,omitempty"`
	Timestamp int64   `json:"timestamp"`
	User      string  `json:"user"`
	Minor     bool    `json:"minor,omitempty"`
	Patrolled bool    `json:"patrolled,omitempty"`
	Length    *OldNew `json:"length"`
	Revision  *OldNew `json:"revision"`
}

func getInt64(val interface{}) int64 {
	if val == nil {
		return -1
	} else {
		return int64(val.(float64))
	}
}

func removeLastFiles(maxCount int) {
	dir := path.Join(utils.DataDir, "db", "events", "raw")
	files_list, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	arr := make([]string, 0)
	for _, fi := range files_list {
		if strings.HasSuffix(fi.Name(), ".json") {
			arr = append(arr, fi.Name())
		}
	}
	sort.Slice(arr, func(i, j int) bool {
		return arr[i] >= arr[j]
	})
	if maxCount < len(arr) {
		arr = arr[maxCount:]
		for _, fname := range arr {
			os.Remove(path.Join(dir, fname))
		}
	}
}

func Main(args []string) bool {
	eventsDaysCount := 64
	if os.Getenv("EVENTS_DAYS") != "" {
		eventsDaysCount = utils.Atoi(os.Getenv("EVENTS_DAYS"))
	}
	removeLastFiles(eventsDaysCount)

	startTime := time.Now().UTC().Format("2006-01-02")
	outFPath := path.Join(utils.DataDir, "db", "events", "raw", startTime+".json")
	f, err := os.OpenFile(outFPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	client := sse.NewClient("https://stream.wikimedia.org/v2/stream/recentchange")

	client.Subscribe("message", func(msg *sse.Event) {
		// fmt.Println( string(msg.Data) )
		var result map[string]interface{}
		json.Unmarshal(msg.Data, &result)
		msg_type := result["type"]
		msg_namespace := result["namespace"]
		msg_bot := result["bot"]
		if (msg_type == "edit" || msg_type == "new") && (msg_namespace == 0.0) && (msg_bot == false) {
			user := ""
			if result["user"] != nil {
				user = result["user"].(string)
			}
			patrolled := false
			if result["patrolled"] != nil {
				patrolled = result["patrolled"].(bool)
			}
			length := result["length"].(map[string]interface{})
			revision := result["revision"].(map[string]interface{})
			event := Event{
				Type:      msg_type.(string),
				Wiki:      result["wiki"].(string),
				Title:     result["title"].(string),
				Comment:   result["comment"].(string),
				Timestamp: int64(result["timestamp"].(float64)),
				User:      user,
				Minor:     result["minor"].(bool),
				Patrolled: patrolled,
				Length: &OldNew{
					Old: getInt64(length["old"]),
					New: getInt64(length["new"])},
				Revision: &OldNew{
					Old: getInt64(revision["old"]),
					New: getInt64(revision["new"])}}
			data, err := json.Marshal(event)
			if err != nil {
				panic(err)
			}
			_, err = fmt.Fprintln(f, string(data))
			if err != nil {
				panic(err)
			}
			if time.Now().UTC().Format("2006-01-02") != startTime {
				f.Sync()
				os.Exit(0)
			}
		}
	})

	return true
}
