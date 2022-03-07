package extract

import (
	"compress/gzip"
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	utils "tiger.com.pl/wikidumptools/utils"
)

var AllArticlesById map[string]string = make(map[string]string)
var AllArticlesByTitle map[string]string = make(map[string]string)
var AllRedirectionsById map[string]*Redirect = make(map[string]*Redirect)
var AllRedirectionsByTitle map[string]*Redirect = make(map[string]*Redirect)
var AllCategoriesById map[string]string = make(map[string]string)
var AllCategoriesByTitle map[string]string = make(map[string]string)

var MissingCounter = make(map[string]int)

func IncMissing(key string) {
	MissingCounter[key] += 1
}

type Column struct {
	Name      string
	Desc      string
	Origin    string
	ForeignTo string // default ""
	Primary   bool   // default false
}

type Table struct {
	Name        string
	Desc        string
	Origin      string
	Columns     []Column
	columnNames []string
	outFile     *os.File
	gzStream    *gzip.Writer
	stmt        *sql.Stmt
}

type Output struct {
	Desc    string
	Tables  map[string]*Table
	buffer  []string
	buffer2 []interface{}
}

var TheOutput Output = Output{
	Desc: "Artykuły (namespace 0), przekierowania (też namespace 0) i kategorie (namespace 14) mają wspólną przestrzeń identyfikatorów.\nTzn. np. nie może być artykułu o tym samym identyfikatorze co kategoria.\nTytuły (artykułów, przekierowań, kategorii) zawsze są z dużej litery, wszystkie spacje zamienione na znak podkreślenia.\nWartości NULL są reprezentowane przez łańcuch pusty.",
	Tables: map[string]*Table{
		"articles": &Table{
			Name:   "articles",
			Desc:   "Lista wszystkich artykułów hasłowych.",
			Origin: "page [https://www.mediawiki.org/wiki/Manual:Page_table]",
			Columns: []Column{
				{Name: "page_id", Desc: "Identyfikator artykułu", Origin: "page.page_id", Primary: true},
				{Name: "page_title", Desc: "Tytuł artykułu", Origin: "page.page_title"},
				{Name: "page_len", Desc: "Długość (w bajtach) artykułu", Origin: "page.page_len"},
				{Name: "page_is_new", Desc: "", Origin: "page.page_is_new"},
				{Name: "page_touched", Desc: "", Origin: "page.page_touched"},
				{Name: "page_links_updated", Desc: "", Origin: "page.page_links_updated"},
				{Name: "page_latest", Desc: "", Origin: "page.page_latest"}}},
		"category_index": &Table{
			Name:   "category_index",
			Desc:   "Lista wszystkich kategorii",
			Origin: "page [https://www.mediawiki.org/wiki/Manual:Page_table]",
			Columns: []Column{
				{Name: "page_id", Desc: "Identyfikator kategorii", Origin: "page.page_id", Primary: true},
				{Name: "page_title", Desc: "Tytuł kategorii", Origin: "page.page_title"}}},
		"redirects": &Table{
			Name:   "redirects",
			Desc:   "Przekierowania, czyli nazwy alternatywne. Np. 'PRL' jest nazwą alternatywną dla artykułu 'Polska Rzeczpospolita Ludowa'.\n    As of August 2007, database dumps for Wikipedia and other Wikimedia projects as provided on https://dumps.wikimedia.org/ have\n    incomplete data in this table: only redirect pages that have been created or edited after summer 2007 are present.\n    For older redirects, resort to using the pagelinks table.",
			Origin: "page [https://www.mediawiki.org/wiki/Manual:Page_table], redirect [https://www.mediawiki.org/wiki/Manual:Redirect_table]",
			Columns: []Column{
				{Name: "redir_id", Desc: "Identyfikator przekierowania", Origin: "redirect.rd_from", Primary: true},                                              // sztuczny
				{Name: "article_id", Desc: "Identyfikator artykułu, do którego prowadzi przekierowanie", Origin: "page.page_id", ForeignTo: "articles(page_id)"}, // sztuczny
				{Name: "redir_title", Desc: "Tytuł przekierowania (np. 'PRL')", Origin: "page.page_title"}}},                                                     // sztuczny
		"geo_tags": &Table{
			Name:   "geo_tags",
			Desc:   "Lista geotagowanych artykułów",
			Origin: "geo_tags [https://www.mediawiki.org/wiki/Extension:GeoData]",
			Columns: []Column{
				{Name: "gt_page_id", Desc: "Identyfikator artykułu", Origin: "geo_tags.gt_page_id", ForeignTo: "articles(page_id)"},
				{Name: "gt_lat", Desc: "Szerokość geograficzna", Origin: "geo_tags.gt_lat"},
				{Name: "gt_lon", Desc: "Długość geograficzna", Origin: "geo_tags.gt_lon"},
				{Name: "gt_globe", Desc: "Planeta, np. 'earth': Ziemia", Origin: "geo_tags.gt_globe"},
				{Name: "gt_primary", Desc: "'0' gdy artykuł luźno związany z miejscem, np. 'mieszkała tu kiedyś Greta Garbo', wpp '1'", Origin: "geo_tags.gt_primary"},
				{Name: "gt_dim", Desc: "Szacowana wielkość obiektu (w m)", Origin: "geo_tags.gt_dim"},
				{Name: "gt_type", Desc: "Typ obiektu - patrz link w opisie tabeli", Origin: "geo_tags.gt_type"},
				{Name: "gt_name", Desc: "Nazwa obiektu", Origin: "geo_tags.gt_name"},
				{Name: "gt_country", Desc: "Kod kraju", Origin: "geo_tags.gt_country"},
				{Name: "gt_region", Desc: "Kod regionu", Origin: "geo_tags.gt_region"}}},
		"page_restrictions": &Table{
			Name:   "page_restrictions",
			Desc:   "Artykuły zabezpieczone przed modyfikacją",
			Origin: "page_restrictions [https://www.mediawiki.org/wiki/Manual:Page_restrictions_table]",
			Columns: []Column{
				{Name: "pr_page", Desc: "Identyfikator artykułu", Origin: "page_restrictions.pr_page", ForeignTo: "articles(page_id)"},
				{Name: "pr_type", Desc: "", Origin: "page_restrictions.pr_type"},
				{Name: "pr_level", Desc: "", Origin: "page_restrictions.pr_level"},
				{Name: "pr_cascade", Desc: "", Origin: "page_restrictions.pr_cascade"},
				{Name: "pr_user", Desc: "", Origin: "page_restrictions.pr_user"},
				{Name: "pr_expiry", Desc: "", Origin: "page_restrictions.pr_expiry"}}},
		"externallinks": &Table{
			Name:   "externallinks",
			Desc:   "Linki prowadzące z artykułów poza Wikipedię",
			Origin: "externallinks [https://www.mediawiki.org/wiki/Manual:Externallinks_table]",
			Columns: []Column{
				{Name: "el_from", Desc: "Identyfikator artykułu w którym występuje link", Origin: "externallinks.el_from", ForeignTo: "articles(page_id)"},
				{Name: "el_to", Desc: "URL linku", Origin: "externallinks.el_to"}}},
		"imagelinks": &Table{
			Name:   "imagelinks",
			Desc:   "Obrazki umieszczone w artykułach. W jednym artykule może być wiele obrazków",
			Origin: "imagelinks [https://www.mediawiki.org/wiki/Manual:Imagelinks_table]",
			Columns: []Column{
				{Name: "il_from", Desc: "Identyfikator artykułu na którym jest obrazek", Origin: "imagelinks.il_from", ForeignTo: "articles(page_id)"},
				{Name: "il_to", Desc: "Tytuł pliku obrazka", Origin: "imagelinks.il_to"}}},
		"article2category": &Table{
			Name:   "article2category",
			Desc:   "Kategorie artykułów. Jeden artykuł może należeć do wielu kategorii",
			Origin: "categorylinks [https://www.mediawiki.org/wiki/Manual:Categorylinks_table]",
			Columns: []Column{
				{Name: "cl_from", Desc: "Identyfikator artykułu", Origin: "categorylinks.cl_from", ForeignTo: "articles(page_id)"},
				{Name: "cl_to", Desc: "Identyfikator kategorii", Origin: "categorylinks.cl_to", ForeignTo: "category_index(page_id)"}}},
		"category2category": &Table{
			Name:   "category2category",
			Desc:   "Rodzice kategorii",
			Origin: "categorylinks [https://www.mediawiki.org/wiki/Manual:Categorylinks_table]",
			Columns: []Column{
				{Name: "cl_from", Desc: "Identyfikator kategorii (dziecka)", Origin: "categorylinks.cl_from", ForeignTo: "category_index(page_id)"},
				{Name: "cl_to", Desc: "Identyfikator kategorii (ojca)", Origin: "categorylinks.cl_to", ForeignTo: "category_index(page_id)"}}},
		"abstracts": &Table{
			Name:   "abstracts",
			Desc:   "Jednozdaniowe skróty artykułów. Często niestety śmieci zawierające znaki takie jak '|={}[]'.",
			Origin: "*-abstract.xml.gz",
			Columns: []Column{
				{Name: "page_id", Desc: "Identyfikator artykułu", Origin: "?", ForeignTo: "articles(page_id)"},
				{Name: "abstract", Desc: "Pierwsze zdanie artykułu", Origin: "?"}}},
		"langlinks_articles": &Table{
			Name:   "langlinks_articles",
			Desc:   "Odpowiedniki artykułu w innych wersjach językowych Wikipedii.\n    Since Wikidata, 'langlinks' table no longer keeps accurate track of the actual interlanguage links in Wikipedia and other Wikimedia wikis!",
			Origin: "langlinks [https://www.mediawiki.org/wiki/Manual:Langlinks_table]",
			Columns: []Column{
				{Name: "ll_from", Desc: "Identyfikator artykułu", Origin: "langlinks.ll_from", ForeignTo: "articles(page_id)"},
				{Name: "ll_lang", Desc: "Kod języka", Origin: "langlinks.ll_lang"},
				{Name: "ll_title", Desc: "Tytuł docelowego artykułu (Uwaga: spacje nie są zamienione na znaki podkreślenia)", Origin: "langlinks.ll_title"}}},
		"langlinks_redirects": &Table{
			Name:   "langlinks_redirects",
			Desc:   "Odpowiedniki przekierowania w innych wersjach językowych Wikipedii.\n    Since Wikidata, 'langlinks' table no longer keeps accurate track of the actual interlanguage links in Wikipedia and other Wikimedia wikis!",
			Origin: "langlinks [https://www.mediawiki.org/wiki/Manual:Langlinks_table]",
			Columns: []Column{
				{Name: "ll_from", Desc: "Identifykator przekierowania", Origin: "langlinks.ll_from", ForeignTo: "redirects(redir_id)"},
				{Name: "ll_lang", Desc: "Kod języka", Origin: "langlinks.ll_lang"},
				{Name: "ll_title", Desc: "Tytuł docelowego artykułu (Uwaga: spacje nie są zamienione na znaki podkreślenia)", Origin: "langlinks.ll_title"}}},
		"langlinks_categories": &Table{
			Name:   "langlinks_categories",
			Desc:   "Odpowiedniki kategorii w innych wersjach językowych Wikipedii.\n    Since Wikidata, 'langlinks' table no longer keeps accurate track of the actual interlanguage links in Wikipedia and other Wikimedia wikis!",
			Origin: "langlinks [https://www.mediawiki.org/wiki/Manual:Langlinks_table]",
			Columns: []Column{
				{Name: "ll_from", Desc: "Identyfikator kategorii", Origin: "langlinks.ll_from", ForeignTo: "category_index(page_id)"},
				{Name: "ll_lang", Desc: "Kod języka", Origin: "langlinks.ll_lang"},
				{Name: "ll_title", Desc: "Tytuł docelowego artykułu (Uwaga: spacje nie są zamienione na znaki podkreślenia, tytuł zawiera prefiks, np. 'Kategoria:' dla ll_lang='pl')", Origin: "langlinks.ll_title"}}},
		"page_props_articles": &Table{
			Name:   "page_props_articles",
			Desc:   "Własności artykułów",
			Origin: "page_props [https://www.mediawiki.org/wiki/Manual:Page_props_table]",
			Columns: []Column{
				{Name: "pp_page", Desc: "Identyfikator artykułu", Origin: "page_props.pp_page", ForeignTo: "articles(page_id)"},
				{Name: "pp_propname", Desc: "Nazwa własności", Origin: "page_props.pp_propname"},
				{Name: "pp_value", Desc: "Wartość własności", Origin: "page_props.pp_value"}}},
		"page_props_redirects": &Table{
			Name:   "page_props_redirects",
			Desc:   "Własności przekierowań",
			Origin: "page_props [https://www.mediawiki.org/wiki/Manual:Page_props_table]",
			Columns: []Column{
				{Name: "pp_page", Desc: "Identyfikator przekierowania", Origin: "page_props.pp_page", ForeignTo: "redirects(redir_id)"},
				{Name: "pp_propname", Desc: "Nazwa własności", Origin: "page_props.pp_propname"},
				{Name: "pp_value", Desc: "Wartość własności", Origin: "page_props.pp_value"}}},
		"page_props_categories": &Table{
			Name:   "page_props_categories",
			Desc:   "Własności kategorii",
			Origin: "page_props [https://www.mediawiki.org/wiki/Manual:Page_props_table]",
			Columns: []Column{
				{Name: "pp_page", Desc: "Identyfikator kategorii", Origin: "page_props.pp_page", ForeignTo: "category_index(page_id)"},
				{Name: "pp_propname", Desc: "Nazwa własności", Origin: "page_props.pp_propname"},
				{Name: "pp_value", Desc: "Wartość własności", Origin: "page_props.pp_value"}}},
		"templatelinks": &Table{
			Name:   "templatelinks",
			Desc:   "tl_from_namespace jest zawsze 0. tl_namespace=10 oznacza Template, 828 to Module (Lua)",
			Origin: "templatelinks [https://www.mediawiki.org/wiki/Manual:Templatelinks_table]",
			Columns: []Column{
				{Name: "tl_from", Desc: "Identyfikator artykułu zawierającego szablon", Origin: "templatelinks.tl_from", ForeignTo: "articles(page_id)"},
				{Name: "tl_namespace", Desc: "Przestrzeń nazw szablonu ('10' lub '828')", Origin: "templatelinks.tl_namespace"},
				{Name: "tl_title", Desc: "Tytuł szablonu, np. 'infobox_Wieś'", Origin: "templatelinks.tl_title"}}},
		"pagelinks": &Table{
			Name:   "pagelinks",
			Desc:   "Powiązania między artykułami. Jeśli po którejś stronie linku jest przekierowanie, to jest ono zastępowane odpowiadającym mu artykułem.",
			Origin: "pagelinks [https://www.mediawiki.org/wiki/Manual:Pagelinks_table]",
			Columns: []Column{
				{Name: "pl_from", Desc: "Identyfikator artykułu zawierającego link do innego artykułu", Origin: "pagelinks.pl_from", ForeignTo: "articles(page_id)"},
				{Name: "to_page_id", Desc: "Identyfikator artykułu docelowego linku", Origin: "-", ForeignTo: "articles(page_id)"}}}}}

func (o *Output) GetFilePath(tableName string) string {
	return fmt.Sprintf("%s/%s.tsv.gz", OutputDir, tableName)
}

func (o *Output) GenerateDoc(outFile *os.File) {
	fmt.Fprintf(outFile, "%s\n", o.Desc)
	fmt.Fprintf(outFile, "Tabele:\n")
	tableNames := make([]string, 0)
	for tableName, _ := range o.Tables {
		tableNames = append(tableNames, tableName)
	}
	sort.Strings(tableNames)
	for _, tableName := range tableNames {
		table := o.Tables[tableName]
		fmt.Fprintf(outFile, "  %s (Pochodzenie: %s)\n", table.Name, table.Origin)
		fmt.Fprintf(outFile, "    %s\n", table.Desc)
		for index, column := range table.Columns {
			columnDesc := fmt.Sprintf(": %s", column.Desc)
			if len(column.Desc) == 0 {
				columnDesc = column.Desc
			}
			fmt.Fprintf(outFile, "    %d. %s (Pochodzenie: %s)%s\n", index+1, column.Name, column.Origin, columnDesc)
		}
	}
}

func (o *Output) WriteRecord(tableName string, p map[string]string) {
	table := o.Tables[tableName]
	if OutType == "tsv" {
		if table.outFile == nil {
			table.outFile, table.gzStream = utils.CreateGzFile(o.GetFilePath(tableName))
			for _, column := range table.Columns {
				table.columnNames = append(table.columnNames, column.Name)
			}
			_, err := fmt.Fprintf(table.gzStream, "%s\n", strings.Join(table.columnNames, "\t"))
			if err != nil {
				panic(err)
			}
			o.buffer = make([]string, 0, 100)
		}
		o.buffer = o.buffer[:0]
		for _, colName := range table.columnNames {
			o.buffer = append(o.buffer, p[colName])
		}
		_, err := fmt.Fprintf(table.gzStream, "%s\n", strings.Join(o.buffer, "\t"))
		if err != nil {
			panic(err)
		}
	} else if OutType == "sqlite" {
		if table.stmt == nil {
			questionMarks := make([]string, 0, len(table.Columns))
			for _, column := range table.Columns {
				table.columnNames = append(table.columnNames, column.Name)
				questionMarks = append(questionMarks, "?")
			}
			sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableName, strings.Join(table.columnNames, ", "), strings.Join(questionMarks, ", "))
			stmt, err := SQLite.Prepare(sql)
			if err != nil {
				panic(err)
			}
			table.stmt = stmt
			o.buffer2 = make([]interface{}, 0, 100)
		}
		o.buffer2 = o.buffer2[:0]
		for _, colName := range table.columnNames {
			o.buffer2 = append(o.buffer2, p[colName])
		}
		_, err := TheTx.Stmt(table.stmt).Exec(o.buffer2...)
		if err != nil {
			panic(err)
		}
		TheTxCounter += 1
		if TheTxCounter == 100000 {
			// fmt.Fprintf( os.Stderr, "@" )
			err = TheTx.Commit()
			if err != nil {
				panic(err)
			}
			TheTx, err = SQLite.Begin()
			if err != nil {
				panic(err)
			}
			TheTxCounter = 0
		}
	}
}

func (o *Output) Close() {
	for _, table := range o.Tables {
		if table.gzStream != nil {
			table.gzStream.Flush()
			table.gzStream.Close()
		}
		if table.outFile != nil {
			table.outFile.Close()
		}
		if table.stmt != nil {
			table.stmt.Close()
		}
	}
	if SQLite != nil {
		if TheTxCounter > 0 {
			err := TheTx.Commit()
			if err != nil {
				panic(err)
			}
		}
		SQLite.Close()
	}
}

type Redirect struct {
	RedirId    string
	RedirTitle string
	ArticleId  string
}

// https://www.mediawiki.org/wiki/Manual:Page_table
func process_page_table(p map[string]string) {
	page_namespace := p["page_namespace"]
	page_title := p["page_title"]
	if (page_title != "") && ((page_namespace == "0") || (page_namespace == "14")) {
		page_id := p["page_id"]
		page_is_redirect := p["page_is_redirect"] == "1"
		if page_namespace == "0" {
			if page_is_redirect {
				redirect := Redirect{
					RedirId:    page_id,
					RedirTitle: page_title,
					ArticleId:  ""}
				AllRedirectionsById[page_id] = &redirect
				AllRedirectionsByTitle[page_title] = &redirect
			} else {
				AllArticlesById[page_id] = page_title
				AllArticlesByTitle[page_title] = page_id
				TheOutput.WriteRecord("articles", p)
			}
		} else {
			if !page_is_redirect {
				AllCategoriesById[page_id] = page_title
				AllCategoriesByTitle[page_title] = page_id
				TheOutput.WriteRecord("category_index", p)
			}
		}
	}
}

// https://www.mediawiki.org/wiki/Manual:Redirect_table
func process_redirect_table(p map[string]string) {
	if p["rd_namespace"] == "0" {
		if page_id, ok := AllArticlesByTitle[p["rd_title"]]; ok {
			rd_from := p["rd_from"]
			if redirection, ok2 := AllRedirectionsById[rd_from]; ok2 {
				redirection.ArticleId = page_id
				p["article_id"] = page_id
				p["redir_id"] = rd_from
				p["redir_title"] = redirection.RedirTitle
				TheOutput.WriteRecord("redirects", p)
			} else {
				IncMissing("redirects_from")
			}
		} else {
			IncMissing("redirects_to")
		}
	}
}

// https://www.mediawiki.org/wiki/Extension:GeoData
func process_geo_tags_table(p map[string]string) {
	if _, ok := AllArticlesById[p["gt_page_id"]]; ok {
		TheOutput.WriteRecord("geo_tags", p)
	} else {
		IncMissing("geo_tags")
	}
}

// https://www.mediawiki.org/wiki/Manual:Page_restrictions_table
func process_page_restrictions_table(p map[string]string) {
	if _, ok := AllArticlesById[p["pr_page"]]; ok {
		TheOutput.WriteRecord("page_restrictions", p)
	} else {
		IncMissing("page_restrictions")
	}
}

// https://www.mediawiki.org/wiki/Manual:Externallinks_table
func process_externallinks_table(p map[string]string) {
	if _, ok := AllArticlesById[p["el_from"]]; ok {
		TheOutput.WriteRecord("externallinks", p)
	} else {
		if _, ok2 := AllRedirectionsById[p["el_from"]]; ok2 {
			IncMissing("externallinks-redirection-hits")
		} else {
			IncMissing("externallinks")
		}
	}
}

// https://www.mediawiki.org/wiki/Manual:Imagelinks_table
func process_imagelinks_table(p map[string]string) {
	if p["il_from_namespace"] == "0" {
		if _, ok := AllArticlesById[p["il_from"]]; ok {
			TheOutput.WriteRecord("imagelinks", p)
		} else {
			IncMissing("imagelinks")
		}
	}
}

// https://www.mediawiki.org/wiki/Manual:Langlinks_table
func process_langlinks_table(p map[string]string) {
	ll_from := p["ll_from"]
	if _, ok := AllArticlesById[ll_from]; ok {
		TheOutput.WriteRecord("langlinks_articles", p)
	} else if _, ok2 := AllCategoriesById[ll_from]; ok2 {
		TheOutput.WriteRecord("langlinks_categories", p)
		IncMissing("langlinks-categories-hits")
	} else if _, ok3 := AllRedirectionsById[ll_from]; ok3 {
		TheOutput.WriteRecord("langlinks_redirects", p)
		IncMissing("langlinks-redirects-hists")
	} else {
		IncMissing("langlinks")
	}
}

// https://www.mediawiki.org/wiki/Manual:Page_props_table
func process_page_props_table(p map[string]string) {
	pp_page := p["pp_page"]
	if _, ok := AllArticlesById[pp_page]; ok {
		TheOutput.WriteRecord("page_props_articles", p)
	} else if _, ok2 := AllCategoriesById[pp_page]; ok2 {
		IncMissing("page_props-categories-hits")
		TheOutput.WriteRecord("page_props_categories", p)
	} else if _, ok3 := AllRedirectionsById[pp_page]; ok3 {
		IncMissing("page_props-redirects-hists")
		TheOutput.WriteRecord("page_props_redirects", p)
	} else {
		IncMissing("page_props")
	}
}

// https://www.mediawiki.org/wiki/Manual:Pagelinks_table
func process_pagelinks_table(p map[string]string) {
	pl_from_namespace := p["pl_from_namespace"]
	pl_namespace := p["pl_namespace"]
	pl_from := p["pl_from"]
	if (pl_from_namespace == "0") && (pl_namespace == "0") {
		_, ok := AllArticlesById[pl_from]
		to_page_id, ok2 := AllArticlesByTitle[p["pl_title"]]
		if ok && ok2 {
			p["to_page_id"] = to_page_id
			TheOutput.WriteRecord("pagelinks", p)
		} else {
			if !ok {
				if redirect, ok3 := AllRedirectionsById[pl_from]; ok3 {
					if redirect.ArticleId != "" {
						p["pl_from"] = redirect.ArticleId
						ok = true
					} else {
						IncMissing("pagelinks-from-redirects-hit")
					}
				} else {
					IncMissing("pagelinks-from")
				}
			}
			if !ok2 {
				if redirect2, ok4 := AllRedirectionsByTitle[p["pl_title"]]; ok4 {
					if (redirect2.ArticleId != "") && (AllArticlesById[redirect2.ArticleId] != "") {
						p["pl_title"] = AllArticlesById[redirect2.ArticleId]
						to_page_id = redirect2.ArticleId
						ok2 = true
					} else {
						IncMissing("pagelinks-to-redirects-hit")
					}
				} else {
					IncMissing("pagelinks-to")
				}
			}
			if ok && ok2 {
				p["to_page_id"] = to_page_id
				TheOutput.WriteRecord("pagelinks", p)
			}
		}
	}
}

// https://www.mediawiki.org/wiki/Manual:Templatelinks_table
func process_templatelinks_table(p map[string]string) {
	tl_from_namespace := p["tl_from_namespace"]
	tl_namespace := p["tl_namespace"]
	if (tl_from_namespace == "0") && ((tl_namespace == "10") || (tl_namespace == "828")) {
		if _, ok := AllArticlesById[p["tl_from"]]; ok {
			TheOutput.WriteRecord("templatelinks", p)
		} else {
			IncMissing("templatelinks")
		}
	}
}

// https://www.mediawiki.org/wiki/Manual:Categorylinks_table
func process_categorylinks_table(p map[string]string) {
	cl_from := p["cl_from"]
	cl_to := p["cl_to"]
	cl_type := p["cl_type"]
	if (cl_from != "") && (cl_to != "") {
		if cl_type == "page" {
			if _, ok := AllArticlesById[cl_from]; ok {
				if cid, ok2 := AllCategoriesByTitle[cl_to]; ok2 {
					p["cl_to"] = cid
					TheOutput.WriteRecord("article2category", p)
				} else {
					IncMissing("categorylinks-page-to")
				}
			} else {
				if _, ok5 := AllRedirectionsById[cl_from]; ok5 {
					IncMissing("categorylinks-page-from-hit-redirect")
				} else {
					IncMissing("categorylinks-page-from")
				}
			}
		} else if cl_type == "subcat" {
			if _, ok3 := AllCategoriesById[cl_from]; ok3 {
				if cid2, ok4 := AllCategoriesByTitle[cl_to]; ok4 {
					p["cl_to"] = cid2
					TheOutput.WriteRecord("category2category", p)
				} else {
					IncMissing("categorylinks-category-to")
				}
			} else {
				IncMissing("categorylinks-category-from")
			}
		}
	}
}

func ProcessAbstractXML(streamName string, r io.ReadCloser) {
	fmt.Fprintf(os.Stderr, "Processing %s... ", streamName)
	defer r.Close()
	p := make(map[string]string)
	record_counter := 0
	inside_url := false
	inside_abstract := false
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			inside_url, inside_abstract = false, false
			if tok.Name.Local == "url" {
				inside_url = true
			} else if tok.Name.Local == "abstract" {
				inside_abstract = true
			}
		case xml.EndElement:
			if tok.Name.Local == "doc" {
				p["page_id"] = ""
				record_counter += 1
			}
		case xml.CharData:
			if inside_url {
				str := string([]byte(tok))
				url, err2 := url.QueryUnescape(str)
				if err2 != nil {
					panic(err2)
				}
				arr := strings.Split(url, "/")
				article_title := arr[len(arr)-1]
				if pid, ok := AllArticlesByTitle[article_title]; ok {
					p["page_id"] = pid
				} else {
					if _, ok2 := AllRedirectionsByTitle[article_title]; ok2 {
						IncMissing("abstract-redirection-hit")
					} else {
						IncMissing("abstract")
					}
				}
				inside_url = false
			} else if inside_abstract && (p["page_id"] != "") {
				str := strings.Replace(string([]byte(tok)), "\t", " ", -1)
				str = strings.Replace(str, "\n", " ", -1)
				p["abstract"] = str
				TheOutput.WriteRecord("abstracts", p)
				inside_abstract = false
			}
		}
	}
	fmt.Fprintf(os.Stderr, " %d\n", record_counter)
}

func processLocalFiles(inputDir string) {
	utils.ProcessLocalFile(path.Join(inputDir, "page.tsv"), process_page_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "redirect.tsv"), process_redirect_table, "\t", nil)
	fpath := path.Join(inputDir, "abstract.xml")
	ProcessAbstractXML(fpath, utils.FileStream(fpath))
	utils.ProcessLocalFile(path.Join(inputDir, "geo_tags.tsv"), process_geo_tags_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "categorylinks.tsv"), process_categorylinks_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "page_restrictions.tsv"), process_page_restrictions_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "externallinks.tsv"), process_externallinks_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "imagelinks.tsv"), process_imagelinks_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "langlinks.tsv"), process_langlinks_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "page_props.tsv"), process_page_props_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "pagelinks.tsv"), process_pagelinks_table, "\t", nil)
	utils.ProcessLocalFile(path.Join(inputDir, "templatelinks.tsv"), process_templatelinks_table, "\t", nil)
}

func processNetFile(fname string, fn utils.ProcessFun) {
	url := fmt.Sprintf("https://dumps.wikimedia.org/%s/%s/%s-%s-%s", LangWiki, utils.DumpDateStr, LangWiki, utils.DumpDateStr, fname)
	stream := utils.HTTPStream(url)
	defer stream.Close()
	utils.ProcessGzippedStream(url, stream, fn)
}

func check(name string) bool {
	names := strings.Split(os.Getenv("INCLUDE_FILES"), ",")
	if os.Getenv("INCLUDE_FILES") == "" {
		return true
	}
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}

func processNetFiles() {
	if check("page") {
		processNetFile("page.sql.gz", process_page_table)
	}
	if check("redirect") {
		processNetFile("redirect.sql.gz", process_redirect_table)
	}
	if check("abstract") {
		url := fmt.Sprintf("https://dumps.wikimedia.org/%s/%s/%s-%s-%s", LangWiki, utils.DumpDateStr, LangWiki, utils.DumpDateStr, "abstract.xml.gz")
		stream := utils.HTTPStream(url)
		gz, err := gzip.NewReader(stream)
		if err != nil {
			panic(err)
		}
		defer gz.Close()
		ProcessAbstractXML(url, gz)
	}
	if check("geo_tags") {
		processNetFile("geo_tags.sql.gz", process_geo_tags_table)
	}
	if check("categorylinks") {
		processNetFile("categorylinks.sql.gz", process_categorylinks_table)
	}
	if check("page_restrictions") {
		processNetFile("page_restrictions.sql.gz", process_page_restrictions_table)
	}
	if check("externallinks") {
		processNetFile("externallinks.sql.gz", process_externallinks_table)
	}
	if check("imagelinks") {
		processNetFile("imagelinks.sql.gz", process_imagelinks_table)
	}
	if check("langlinks") {
		processNetFile("langlinks.sql.gz", process_langlinks_table)
	}
	if check("page_props") {
		processNetFile("page_props.sql.gz", process_page_props_table)
	}
	if check("pagelinks") {
		processNetFile("pagelinks.sql.gz", process_pagelinks_table)
	}
	if check("templatelinks") {
		processNetFile("templatelinks.sql.gz", process_templatelinks_table)
	}
}

var Cmd string
var LangWiki string
var OutType string
var LangCode string
var OutputDir string
var SQLite *sql.DB
var TheTx *sql.Tx
var TheTxCounter int = 0

func ExecSQL(sql string) {
	stmt, err := SQLite.Prepare(sql)
	if err != nil {
		panic(err)
	}
	_, err = stmt.Exec()
	if err != nil {
		panic(err)
	}
	stmt.Close()
}

func Main(args []string) bool {
	if len(args) == 2 && args[1] == "doc" {
		TheOutput.GenerateDoc(os.Stdout)
		os.Exit(0)
	} else if len(args) == 4 {
		Cmd = args[1]
		LangWiki = args[2]
		OutType = args[3]
		if ((Cmd != "net") && (Cmd != "local")) || ((OutType != "tsv") && (OutType != "sqlite")) || (strings.TrimSpace(LangWiki) == "") {
			return false
		}
		LangCode, OutputDir = utils.ParseLangWiki(LangWiki, true)
		if OutType == "tsv" {
			os.Mkdir(OutputDir, 0777)
			files, err := filepath.Glob(path.Join(OutputDir, "*.gz"))
			if err != nil {
				panic(err)
			}
			for _, f := range files {
				if err := os.Remove(f); err != nil {
					panic(err)
				}
			}
		} else {
			db_path := path.Join(OutputDir, fmt.Sprintf("%s.db", LangCode))
			if _, err := os.Stat(db_path); err == nil {
				fmt.Fprintf(os.Stderr, "Remove file %s\n", db_path)
				err = os.Remove(db_path)
				if err != nil {
					panic(err)
				}
			}
			fmt.Fprintf(os.Stderr, "Create database %s\n", db_path)
			db, err := sql.Open("sqlite3", db_path)
			if err != nil {
				panic(err)
			}
			SQLite = db
			ExecSQL("PRAGMA foreign_keys=OFF")
			for tableName, table := range TheOutput.Tables {
				columnNames := make([]string, 0, len(table.Columns))
				for _, column := range table.Columns {
					primaryDef := ""
					if column.Primary {
						primaryDef = " PRIMARY KEY"
					}
					columnNames = append(columnNames, fmt.Sprintf("%s TEXT%s", column.Name, primaryDef))
				}
				for _, column := range table.Columns {
					if column.ForeignTo != "" {
						columnNames = append(columnNames, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s", column.Name, column.ForeignTo))
					}
				}
				sql := fmt.Sprintf("CREATE TABLE %s (%s)", tableName, strings.Join(columnNames, ", "))
				ExecSQL(sql)
				fmt.Fprintf(os.Stderr, "%s\n", sql)
				tx, err := db.Begin()
				if err != nil {
					panic(err)
				}
				TheTx = tx
			}
		}
		if Cmd == "local" {
			processLocalFiles(utils.DataDir)
		} else {
			processNetFiles()
		}
	} else {
		return false
	}
	// url := "https://dumps.wikimedia.org/plwiki/latest/plwiki-latest-protected_titles.sql.gz"
	// ProcessGzippedStream( url, HTTPStream( url ), process, nil )
	// ProcessTsvFile( "data/protected_titles.tsv", "protected_titles", process, nil )
	fmt.Fprintf(os.Stderr, "Stats:\n")
	for key, value := range MissingCounter {
		fmt.Fprintf(os.Stderr, "  %s: %d\n", key, value)
	}
	TheOutput.Close()
	return true
}
