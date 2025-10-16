package pageview_hour

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	utils "wikidumptools/utils"

	synthesis "wikidumptools/synthesis"

	cathier "wikidumptools/cathier"
)

const LastHoursCount int = 24
const TopCount int = 1000

var DomainRe *regexp.Regexp

func dumpDoc() {
	fmt.Fprintf(os.Stdout, "%s\n", ``)
}

type Redirect struct {
	RedirectId    string
	ArticleId     int64
	RedirectTitle string
}

type Article struct {
	PageId int64
	Title  string
}

var RedirectsByTitle map[string]*Redirect = make(map[string]*Redirect)
var ArticlesById map[int64]*Article = make(map[int64]*Article)
var ArticlesByTitle map[string]*Article = make(map[string]*Article)

func process_redirects_table(p map[string]string) {
	redir_title := p["redir_title"]
	redir := Redirect{
		RedirectId:    p["redir_id"],
		ArticleId:     utils.Atoi64(p["article_id"]),
		RedirectTitle: redir_title}
	RedirectsByTitle[redir_title] = &redir
}

func process_articles_table(p map[string]string) {
	page_id := utils.Atoi64(p["page_id"])
	page_title := p["page_title"]
	article := Article{
		PageId: page_id,
		Title:  page_title}
	ArticlesById[page_id] = &article
	ArticlesByTitle[page_title] = &article
}

var Level int
var MaxLevels = 5
var TheAcc map[string][]int
var LangCode string
var LangCode2 string
var ParamHash map[string]int = map[string]int{"domain_code": 0, "page_title": 1, "count_views": 2, "total_response_size": 3}

func process_hour_pageview_table(p map[string]string) {
	domain_code := p["domain_code"]
	if (domain_code == LangCode) || (domain_code == LangCode2) {
		page_title := p["page_title"]
		article, ok := ArticlesByTitle[page_title]
		if !ok {
			redirect, ok2 := RedirectsByTitle[page_title]
			if ok2 {
				article = ArticlesById[redirect.ArticleId]
			}
		}
		if article != nil {
			count_views, _ := strconv.Atoi(p["count_views"])
			if _, ok := TheAcc[page_title]; !ok {
				TheAcc[page_title] = make([]int, MaxLevels)
			}
			TheAcc[page_title][Level] += count_views
		}
	}
}

type Result struct {
	Timestamp  string               `json:"ts_utc"`
	Articles   []*synthesis.Article `json:"articles"`
	Categories []*cathier.Category  `json:"categories,omitempty"`
}

func generateTop(timestamp time.Time) {
	// Wybieramy TopCount najlepszych kluczy TheAcc
	fmt.Fprintf(os.Stderr, "Looking for best pages in each level... ")
	titles := make(map[string]bool)
	keys := []string{}
	for key, values := range TheAcc {
		if values[MaxLevels-1] >= 10 {
			keys = append(keys, key)
		}
	}
	for level := 0; level < MaxLevels; level++ {
		fmt.Fprintf(os.Stderr, "%d ", level+1)
		sort.Slice(keys, func(i, j int) bool { return !(TheAcc[keys[i]][level] < TheAcc[keys[j]][level]) })
		keys2 := keys
		if len(keys) > TopCount {
			keys2 = keys[:TopCount]
		}
		for _, key := range keys2 {
			titles[key] = true
		}
	}
	fmt.Fprintf(os.Stderr, "DONE\n")
	page_ids := []int64{}
	for title, _ := range titles {
		article := ArticlesByTitle[title]
		if article != nil {
			page_ids = append(page_ids, article.PageId)
		}
	}
	synthesis.SetArticleIds(page_ids)
	synthesis.Generate(fmt.Sprintf("%swiki", LangCode), nil)
	result := Result{
		Timestamp:  timestamp.Format("2006-01-02 15:04:05"),
		Articles:   make([]*synthesis.Article, 0),
		Categories: synthesis.CategoryGraphToSlice()}
	for _, s_article := range synthesis.AllArticlesById {
		if s_article != nil {
			article := ArticlesByTitle[s_article.Title]
			if article != nil {
				oldVal := s_article.Views[0]
				s_article.Views = TheAcc[s_article.Title]
				s_article.Views = append(s_article.Views, oldVal)
				// fmt.Println( s_article.Title, s_article.Views )
			}
		}
	}
	for _, page_id := range page_ids {
		s_article := synthesis.AllArticlesById[page_id]
		if s_article != nil {
			result.Articles = append(result.Articles, s_article)
		}
	}

	_, LangPublicDataDir := utils.ParsePublicLangWiki(fmt.Sprintf("%swiki", LangCode), true)
	outputDir := path.Join(LangPublicDataDir)
	os.MkdirAll(outputDir, 0777)

	outputFPath := path.Join(outputDir, "hour_pageview.json")
	out, err := ioutil.TempFile(outputDir, "tmp")
	if err != nil {
		panic(err)
	}
	defer os.Remove(out.Name())
	os.Chmod(out.Name(), 0755)
	enc := json.NewEncoder(out)
	enc.Encode(result)
	out.Close()
	err = os.Rename(out.Name(), outputFPath)
	if err != nil {
		panic(err)
	}
	err = os.Chmod(outputFPath, 0755)
	if err != nil {
		panic(err)
	}

	result.Categories = nil
	for _, article := range result.Articles {
		article.Cats = nil
		article.PageLinks = nil
		article.Prev = nil
		article.Next = nil
	}
	outputFPath = path.Join(outputDir, "hour_pageview_simple.json")
	out2, err := ioutil.TempFile(outputDir, "tmp")
	if err != nil {
		panic(err)
	}
	defer os.Remove(out2.Name())
	os.Chmod(out2.Name(), 0755)
	enc = json.NewEncoder(out2)
	enc.Encode(result)
	out2.Close()
	err = os.Rename(out2.Name(), outputFPath)
	if err != nil {
		panic(err)
	}
	err = os.Chmod(outputFPath, 0755)
	if err != nil {
		panic(err)
	}
}

func generateHourPageview(rawFilesDir string, fnames []string, lang_code string) {
	fmt.Fprintf(os.Stderr, "Processing language '%s'...\n", lang_code)
	Level = 0
	TheAcc = make(map[string][]int)
	ArticlesByTitle = make(map[string]*Article)
	ArticlesById = make(map[int64]*Article)
	RedirectsByTitle = make(map[string]*Redirect)
	LangCode = lang_code
	LangCode2 = fmt.Sprintf("%s.m", lang_code)
	runtime.GC()

	_, langDataDir := utils.ParseLangWiki(lang_code+"wiki", false)
	utils.ProcessLocalFile(path.Join(langDataDir, "articles.tsv"), process_articles_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(langDataDir, "redirects.tsv"), process_redirects_table, "\t", nil)

	for index, fname := range fnames {
		hours := index + 1
		switch {
		case hours <= 1:
			Level = 0
		case hours <= 3:
			Level = 1
		case hours <= 6:
			Level = 2
		case hours <= 12:
			Level = 3
		case hours <= 24:
			Level = 4
		default:
			panic("Level too high")
		}
		utils.ProcessLocalFile(path.Join(rawFilesDir, strings.Replace(fname, ".gz", "", -1)), process_hour_pageview_table, " ", ParamHash)
	}
	fmt.Fprintf(os.Stderr, "Accumulate pageviews from all levels... ")
	for _, values := range TheAcc {
		for i := 1; i < len(values); i++ {
			values[i] = values[i] + values[i-1]
		}
	}
	fmt.Fprintf(os.Stderr, "DONE\n")
	timestamp := timeFromFName(fnames[0])
	generateTop(timestamp)
}

func timeFromFName(fname string) time.Time {
	re, err := regexp.Compile(`pageviews-(\d{4})(\d{2})(\d{2})-(\d{2})`)
	if err != nil {
		panic(err)
	}
	submatch := re.FindStringSubmatch(fname)
	if submatch != nil {
		year, _ := strconv.Atoi(submatch[1])
		month, _ := strconv.Atoi(submatch[2])
		day, _ := strconv.Atoi(submatch[3])
		hour, _ := strconv.Atoi(submatch[4])
		return time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
	} else {
		panic("Cannot parse time in '" + fname + "'")
	}
}

// https://dumps.wikimedia.org/other/pageviews/2019/2019-05/pageviews-20190505-170000.gz
func urlForTime(t time.Time) *url.URL {
	result, _ := url.Parse(fmt.Sprintf("https://dumps.wikimedia.org/other/pageviews/%d/%d-%02d/pageviews-%d%02d%02d-%02d0000.gz", t.Year(), t.Year(), t.Month(), t.Year(), t.Month(), t.Day(), t.Hour()))
	return result
}

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

func DownloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%d", resp.StatusCode)
	}
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		panic(err)
	}
	defer gzr.Close()
	out, err := ioutil.TempFile(path.Dir(filepath), "tmp")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	gzw := gzip.NewWriter(out)

	utils.ProcessTsvFile("", gzr, func(p map[string]string) {
		domain_code := p["domain_code"]
		if DomainRe.FindString(domain_code) != "" {
			_, err := fmt.Fprintf(gzw, "%s %s %s %d\n", domain_code, p["page_title"], p["count_views"], 0)
			if err != nil {
				panic(err)
			}
		}
	}, " ", ParamHash)

	gzw.Flush()
	gzw.Close()
	out.Close()
	err = os.Rename(out.Name(), filepath)
	return err
}

var DownloadsCount int = 0

func downloadFile(filepath string, url string) error {
	fmt.Fprintf(os.Stderr, "Downloading '%s'... ", url)
	err := DownloadFile(filepath, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	} else {
		DownloadsCount += 1
		// fmt.Fprintf( os.Stderr, "OK\n" )
	}
	return err
}

func Main(args []string) bool {
	synthesis.GenerateCategoriesFlag = true
	synthesis.DisableFlags = map[string]bool{"page_restrictions": true, "displaytitle": true, "wikidata_id": true, "extract": true, "redirects": true, "externallinks": true, "modules": true, "imagelinks": true, "extra": true}

	if len(args) < 1 {
		return false
	}

	var buffer bytes.Buffer
	buffer.WriteString("^(")
	arr := []string{}
	for lang_code, _ := range utils.SupportedLanguages {
		arr = append(arr, lang_code)
	}
	buffer.WriteString(strings.Join(arr, "|"))
	buffer.WriteString(")(\\.|$)")
	var err error
	DomainRe, err = regexp.Compile(buffer.String())
	if err != nil {
		panic(err)
	}

	rawFilesDir := path.Join(utils.DataDir, "cache", "pageview", "hours")
	os.MkdirAll(rawFilesDir, 0777)

	// usuwamy smieci (pliki tmp*)
	toRemove := []string{}
	files, err := ioutil.ReadDir(rawFilesDir)
	if err != nil {
		panic(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "tmp") {
			toRemove = append(toRemove, f.Name())
		}
	}
	for _, fname := range toRemove {
		os.Remove(path.Join(rawFilesDir, fname))
	}

	// dla każdej wersji językowej Wikipedii w ${DATA_DIR}/cache/dumps/wikipedia/XX wygeneruj pliki 1000 najpopularniejszych artykułów z ostatniej 1,3,6,12 i 24h
	files_list, err := ioutil.ReadDir(path.Join(utils.DataDir, "cache", "dumps", "wikipedia"))
	if err != nil {
		panic(err)
	}
	for _, fi := range files_list {
		if fi.IsDir() && (len(fi.Name()) <= 3) && utils.SupportedLanguages[fi.Name()] {
			lang_code, langDataDir := utils.ParseLangWiki(fi.Name()+"wiki", false)
			if _, err := os.Stat(path.Join(langDataDir, "geo_tags.tsv.gz")); err == nil {
				// dociagamy brakujace LastHoursCount godzin
				curr_time := time.Now().UTC().Truncate(time.Hour)
				lastCount := 0
				index := 0
				for lastCount < LastHoursCount && index < 30 {
					url := urlForTime(curr_time)
					fname := path.Base(url.Path)
					fpath := path.Join(rawFilesDir, fname)
					if exist(fpath) || (downloadFile(fpath, url.String()) == nil) {
						lastCount += 1
					}

					index += 1
					curr_time = curr_time.Add(-time.Hour)
				}

				// usuwamy nadmiarowe pliki, zostawiamy LastHoursCount najświeższych ostatnich plikow *.gz
				fnames := []string{}
				files, err = ioutil.ReadDir(rawFilesDir)
				if err != nil {
					panic(err)
				}
				for _, f := range files {
					if strings.HasSuffix(f.Name(), ".gz") {
						fnames = append(fnames, f.Name())
					}
				}
				sort.Slice(fnames, func(i, j int) bool { return !(fnames[i] < fnames[j]) })
				var fname string
				for len(fnames) > LastHoursCount {
					fname, fnames = fnames[len(fnames)-1], fnames[:len(fnames)-1]
					os.Remove(path.Join(rawFilesDir, fname))
				}

				// przetwarzamy wszystkie pliki
				generateHourPageview(rawFilesDir, fnames, lang_code)
			}
		}
	}

	return true
}
