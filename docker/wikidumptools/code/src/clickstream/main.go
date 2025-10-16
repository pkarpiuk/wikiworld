package clickstream

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"

	utils "wikidumptools/utils"
)

func dumpDoc() {
	fmt.Fprintf(os.Stdout, "%s\n", "_id (Int): identyfikator artykułu (page_id)")
	// fmt.Fprintf( os.Stdout, "%s\n", "title (String): tytuł artykułu (znaki spacji zamienione na znaki podkreślenia)" )
	fmt.Fprintf(os.Stdout, "%s\n", "prev (Object): skąd użytkownicy przyszli do artykułu")
	fmt.Fprintf(os.Stdout, "%s\n", "  <page_id> (String): page_id artykułu źródłowego lub jeden z identyfikatorów 'other-*'")
	fmt.Fprintf(os.Stdout, "%s\n", "                    (patrz https://meta.wikimedia.org/wiki/Research:Wikipedia_clickstream#Data_Preparation)")
	fmt.Fprintf(os.Stdout, "%s\n", "                    Identyfikator other-search-internal oznacza że artykuł najprawdopodobniej został znaleziony w wyszukiwarce Wikipedii")
	fmt.Fprintf(os.Stdout, "%s\n", "  <count> (Int): Liczba wejść z danego źródła (zawsze >= 10)")
	fmt.Fprintf(os.Stdout, "%s\n", "next (Object): do jakiego artykułu użytkownicy przeszli")
	fmt.Fprintf(os.Stdout, "%s\n", "  <page_id> (String): page_id artykułu docelowego")
	fmt.Fprintf(os.Stdout, "%s\n", "  <count> (Int): Liczba wyjść do danego artykułu docelowego (zawsze >= 10)")
}

const MaxLinksCount int = 20

type Article struct {
	PageId int `json:"_id"`
	// Title string         `json:"title"`
	Prev map[string]int `json:"prev,omitempty"`
	Next map[string]int `json:"next,omitempty"`
}

func (a *Article) IsEmpty() bool {
	return (len(a.Prev) == 0) && (len(a.Next) == 0)
}

func (a *Article) TruncMap(hash map[string]int, maxCount int) {
	values := []int{}
	for _, value := range hash {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return !(values[i] < values[j]) })
	if len(values) > maxCount {
		min := values[maxCount-1]
		keysToDelete := []string{}
		for key, value := range hash {
			if value < min {
				keysToDelete = append(keysToDelete, key)
			}
		}
		for _, key := range keysToDelete {
			delete(hash, key)
		}
	}
}

type Redirect struct {
	RedirectId    string
	ArticleId     string
	RedirectTitle string
}

var LangWiki string
var AllArticlesByTitle map[string]*Article
var AllArticlesById map[string]*Article
var AllRedirectsByTitle map[string]*Redirect

type ProcessFun func(article *Article)

func LoadClickstream(fpath string, fn ProcessFun) {
	fmt.Fprintf(os.Stderr, "Loading %s...\n", fpath)
	var r io.ReadCloser
	// sprawdzamy czy jest wersja spakowana
	gfpath := fpath + ".gz"
	if _, err := os.Stat(gfpath); err == nil {
		fpath = gfpath
	}
	file, err := os.Open(fpath)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if fpath == gfpath {
		gz, err := gzip.NewReader(file)
		if err != nil {
			panic(err)
		}
		defer gz.Close()
		r = gz
	} else {
		r = file
	}
	dec := json.NewDecoder(r)
	for {
		var article Article
		if err := dec.Decode(&article); err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}

		fn(&article)
	}
}

func process_redirects_table(p map[string]string) {
	redir_title := p["redir_title"]
	redir := Redirect{
		RedirectId:    p["redir_id"],
		ArticleId:     p["article_id"],
		RedirectTitle: redir_title}
	AllRedirectsByTitle[redir_title] = &redir
}

func process_articles_table(p map[string]string) {
	page_id := p["page_id"]
	page_id_int, _ := strconv.Atoi(page_id)
	page_title := p["page_title"]
	article := Article{
		PageId: page_id_int,
		// Title: page_title,
		Prev: make(map[string]int),
		Next: make(map[string]int)}
	AllArticlesById[page_id] = &article
	AllArticlesByTitle[page_title] = &article
}

func findArticleByTitle(title string) *Article {
	if strings.HasPrefix(title, "other-") {
		return nil
	}
	article := AllArticlesByTitle[title]
	if article == nil {
		redirect := AllRedirectsByTitle[title]
		if redirect != nil {
			article = AllArticlesById[redirect.ArticleId]
			// fmt.Fprintf( os.Stderr, "REDIRECT\n" )
		}
	}
	return article
}

func process_clickstream_table(p map[string]string) {
	c_prev := p["prev"]
	c_curr := p["curr"]
	c_type := p["type"]
	c_n := p["n"]
	if c_type == "other" {
		c_prev = "other-search-internal"
	}
	count, _ := strconv.Atoi(c_n)
	c_curr_article := findArticleByTitle(c_curr)
	c_prev_article := findArticleByTitle(c_prev)
	if c_curr_article != nil {
		if c_prev_article != nil {
			c_curr_article.Prev[fmt.Sprintf("%d", c_prev_article.PageId)] += count
		} else if strings.HasPrefix(c_prev, "other-") {
			c_curr_article.Prev[c_prev] += count
		} else {
			fmt.Fprintf(os.Stderr, "Unknown prev: '%s'\n", c_prev)
		}
	}
	if (c_prev_article != nil) && (c_curr_article != nil) {
		c_prev_article.Next[fmt.Sprintf("%d", c_curr_article.PageId)] += count
	}
}

func processLangCode(lang_code string, outputLangDir string) (errMsg string) {
	clickstreamDir := path.Join(utils.DataDir, "cache", "clickstream")
	if _, err := os.Stat(clickstreamDir); os.IsNotExist(err) {
		return "Directory '" + clickstreamDir + "' doesn't exist"
	}

	AllArticlesByTitle = make(map[string]*Article)
	AllArticlesById = make(map[string]*Article)
	AllRedirectsByTitle = make(map[string]*Redirect)
	runtime.GC()

	utils.ProcessLocalFile(path.Join(outputLangDir, "articles.tsv"), process_articles_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(outputLangDir, "redirects.tsv"), process_redirects_table, "\t", nil)

	files, err := ioutil.ReadDir(clickstreamDir)
	if err != nil {
		panic(err)
	}
	paramHash := map[string]int{"prev": 0, "curr": 1, "type": 2, "n": 3}
	for _, f := range files { // coś w rodzaju ["2019-11", ...]
		if f.IsDir() && (len(f.Name()) == 7) {
			monthDir := path.Join(clickstreamDir, f.Name())
			files2, err := ioutil.ReadDir(monthDir)
			if err != nil {
				panic(err)
			}
			for _, f2 := range files2 {
				if strings.Index(f2.Name(), "-"+LangWiki+"-") >= 0 && strings.HasSuffix(f2.Name(), ".tsv.gz") {
					utils.ProcessLocalFile(path.Join(monthDir, strings.Replace(f2.Name(), ".gz", "", -1)), process_clickstream_table, "\t", paramHash)
				}
			}
		}
	}
	// zrzucamy wynik do pliku JSON
	maxLen := 0
	f, gz := utils.CreateGzFile(path.Join(outputLangDir, "clickstream-articles.json.gz"))
	enc := json.NewEncoder(gz)
	for _, article := range AllArticlesByTitle {
		if !article.IsEmpty() {
			article.TruncMap(article.Prev, MaxLinksCount)
			article.TruncMap(article.Next, MaxLinksCount)
			enc.Encode(article)
			if len(article.Prev) > maxLen {
				maxLen = len(article.Prev)
			}
			if len(article.Next) > maxLen {
				maxLen = len(article.Next)
			}
		}
	}
	gz.Flush()
	gz.Close()
	f.Close()
	fmt.Fprintf(os.Stderr, "Max Len: %d\n", maxLen)
	return
}

func Main(args []string) bool {
	if len(args) < 2 {
		return false
	}
	LangWiki = args[1]
	if LangWiki == "doc" {
		dumpDoc()
		return true
	}
	LangCode, outputLangDir := utils.ParseLangWiki(LangWiki, false)
	err_msg := processLangCode(LangCode, outputLangDir)
	if err_msg != "" {
		panic(err_msg)
	}

	return true
}
