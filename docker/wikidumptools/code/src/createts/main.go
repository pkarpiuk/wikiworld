package createts

import (
	"encoding/csv"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"time"

	utils "wikidumptools/utils"
)

var LangWiki string
var LangCode string
var LangDataDir string
var TsRegexp *regexp.Regexp
var File *os.File

var Cache map[int64]string = make(map[int64]string)

func exist(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	panic(err)
}

func mkdir(dir string) {
	err := os.MkdirAll(dir, 0777)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create dir '%s'", dir)
		os.Exit(1)
	}
}

func readTsv(fpath string) {
	csvFile, err := os.Open(fpath)
	if err != nil {
		fmt.Println(err)
	}
	defer csvFile.Close()
	reader := csv.NewReader(csvFile)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 2
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		Cache[utils.Atoi64(record[0])] = record[1]
	}
}

func downloadURL(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return string(body)
}

func process_articles_table(p map[string]string) {
	Process(utils.Atoi64(p["page_id"]))
}

func Process(page_id int64) *time.Time {
	var result *time.Time = nil
	str, ok := Cache[page_id]
	if !ok {
		url := fmt.Sprintf("https://%s.wikipedia.org/w/api.php?action=query&prop=revisions&rvlimit=1&rvprop=timestamp&rvdir=newer&format=json&formatversion=2&utf8=&pageids=%s", LangCode, page_id)
		json := downloadURL(url)
		submatch := TsRegexp.FindStringSubmatch(json)
		tsStr := ""
		if submatch != nil {
			tsStr = submatch[1]
		}
		if tsStr != "" {
			var err error
			t, err := time.Parse("2006-01-02T15:04:05Z", tsStr)
			if err == nil {
				result = &t
				Cache[page_id] = tsStr
				// fmt.Fprintf( os.Stderr, "\n  %s %s", page_id, tsStr )
				if _, err := fmt.Fprintf(File, "%s\t%s\n", page_id, tsStr); err != nil {
					panic(err)
				}
			} else {
				panic(err)
			}
		}
	} else {
		// fmt.Fprintf( os.Stderr, "%s\n", str )
		t, err := time.Parse("2006-01-02T15:04:05Z", str)
		if err != nil {
			panic(err)
		}
		result = &t
	}
	return result
}

func Begin(langWiki string) {
	LangWiki = langWiki
	LangCode, LangDataDir = utils.ParseLangWiki(LangWiki, false)

	dir := path.Join(utils.DataDir, "wikipedia", "createts")
	if !exist(dir) {
		mkdir(dir)
	}
	fpath := path.Join(dir, LangCode+".tsv")
	if exist(fpath) {
		readTsv(fpath)
	} else {
		file, err := os.Create(fpath)
		if err != nil {
			panic(err)
		}
		file.Close()
	}
	// fmt.Fprintf( os.Stderr, "%s\n", dir )

	var err error
	File, err = os.OpenFile(fpath, os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		panic(err)
	}
	// fmt.Fprintf( os.Stderr, "%s\n", fpath )
	TsRegexp, _ = regexp.Compile(`"timestamp":"([^"]*)"`)
}

func End() {
	if File != nil {
		File.Close()
	}
}

func Main(args []string) bool {
	if len(args) < 2 {
		return false
	}
	LangWiki = args[1]
	Begin(LangWiki)
	utils.ProcessLocalFile(path.Join(LangDataDir, "articles.tsv"), process_articles_table, "\t", nil)
	End()

	return true
}
