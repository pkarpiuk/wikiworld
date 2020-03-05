package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	utils "tiger.com.pl/wikidumptools/utils"

	extract "tiger.com.pl/wikidumptools/extract"

	cathier "tiger.com.pl/wikidumptools/cathier"

	synthesis "tiger.com.pl/wikidumptools/synthesis"

	pageview "tiger.com.pl/wikidumptools/pageview"

	pageview_hour "tiger.com.pl/wikidumptools/pageview_hour"

	geomap "tiger.com.pl/wikidumptools/geomap"

	clickstream "tiger.com.pl/wikidumptools/clickstream"

	monitor "tiger.com.pl/wikidumptools/monitor"

	createts "tiger.com.pl/wikidumptools/createts"

	extra_download "tiger.com.pl/wikidumptools/extra_download"

	events "tiger.com.pl/wikidumptools/events"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Parametry wywołania:\n")
	fmt.Fprintf(os.Stderr, "  [global-options] <cmd> [command-options]\n")
	fmt.Fprintf(os.Stderr, "Dostępne opcje globalne:\n")
	fmt.Fprintf(os.Stderr, "  --dump=20191220: na jakim dumpie działamy (data w formacie YYYYMMDD), albo 'current' (domyślnie 'current')\n")
	fmt.Fprintf(os.Stderr, "Polecenia:\n")
	fmt.Fprintf(os.Stderr, "  sql2tsv < plwiki-latest-category.sql.gz > out.tsv.gz\n")
	fmt.Fprintf(os.Stderr, "    Przekształca dump file *.sql.gz Wikipedii (np. z https://dumps.wikimedia.org/plwiki/ dla polskiej Wikipedii) na *.tsv.gz\n")
	fmt.Fprintf(os.Stderr, "  extract net plwiki tsv\n")
	fmt.Fprintf(os.Stderr, "    Ściąga pliki plwiki z https://dumps.wikimedia.org/plwiki/, przetwarza je i zapisuje wyniki w plikach *.tsv w katalogu $DATA_DIR/cache/dumps/wikipedia/pl/<date>\n")
	fmt.Fprintf(os.Stderr, "    Zamiast 'tsv' może być 'sqlite', wtedy wynik w pliku $DATA_DIR/cache/dumps/wikipedia/<date>/pl.db\n")
	fmt.Fprintf(os.Stderr, "  extract doc\n")
	fmt.Fprintf(os.Stderr, "    Wyrzuca na STDOUT opis generowanych danych wynikowych\n")
	fmt.Fprintf(os.Stderr, "  months-pageview plwiki [N]\n")
	fmt.Fprintf(os.Stderr, "    Generuje w $DATA_DIR/cache/dumps/wikipedia/pl/<ver>/ plik 'months_pageview_articles.tsv.gz' gdzie każdemu artykułowi jest\n")
	fmt.Fprintf(os.Stderr, "    przyporządkowana liczba odwiedzin w ciągu ostatnich N miesięcy (domyślnie 12).\n")
	fmt.Fprintf(os.Stderr, "    Konieczne jest wcześniejsze wywołanie 'extract' i wiki-extra-download pageview-months M, gdzie M >= N\n")
	fmt.Fprintf(os.Stderr, "  cathier plwiki\n")
	fmt.Fprintf(os.Stderr, "    Przetwarza dotyczące kategorii pliki $DATA_DIR/cache/dumps/wikipedia/pl/<date>/* wygenerowane przez 'extract' i zapisuje wynik w tym samym katalogu w pliku categories.json.gz\n")
	fmt.Fprintf(os.Stderr, "    Znajduje korzeń grafu, rozrywa cykle, usuwa kategorie niedostępne z korzenia i rekurencyjnie usuwa liście bez artykułów\n")
	fmt.Fprintf(os.Stderr, "  cathier plwiki full\n")
	fmt.Fprintf(os.Stderr, "    j.w., ale dodatkowo wylicza liczbę artykułów w każdej kategorii i poddrzewie wyznaczanym przez każdą kategorię.\n")
	fmt.Fprintf(os.Stderr, "    Wymaga dużej ilości RAM, np. dla enwiki ok. 12GB. Użyj zmiennej środowiskowej GOMAXPROCS do redukcji liczby wykorzystanych rdzeni CPU (domyślnie brane są wszystkie).\n")
	fmt.Fprintf(os.Stderr, "  cathier doc\n")
	fmt.Fprintf(os.Stderr, "    Dokumentacja formatu generowanego przez 'cathier'\n")
	fmt.Fprintf(os.Stderr, "  synthesis plwiki < pageids.txt > out.json\n")
	fmt.Fprintf(os.Stderr, "    Wszystko co wiemy o wskazanych artykułach podanej wersji językowej Wikipedii + graf kategorii. Konieczne wcześniejesze wywołanie 'extract', 'year-pageview' i 'cathier'\n")
	fmt.Fprintf(os.Stderr, "    W wynikowym pliku *.json każdy rekord jest w osobnym wierszu. Najpierw zrzucane są wszystkie artykuły, potem kategorie\n")
	fmt.Fprintf(os.Stderr, "    Kategorie są zrzucane w kolejności obchodzenia grafu w głąb począwszy od korzenia.\n")
	fmt.Fprintf(os.Stderr, "  synthesis doc\n")
	fmt.Fprintf(os.Stderr, "    Opis formatu danych generowanych przez powyższe polecenie\n")
	fmt.Fprintf(os.Stderr, "  geomap plwiki\n")
	fmt.Fprintf(os.Stderr, "    Generuje w $DATA_DIR/public/wikipedia/pl trzy pliki *.png pokazujące na mapie artykuły mające reprezentację geograficzną\n")
	fmt.Fprintf(os.Stderr, "    Konieczne jest wcześniejsze wywołanie 'extract' i 'months-pageview'\n")
	fmt.Fprintf(os.Stderr, "  clickstream plwiki\n")
	fmt.Fprintf(os.Stderr, "    Generuje w $DATA_DIR/cache/dumps/wikipedia/pl plik 'clickstream-articles.json.gz' w którym dla każdego artykułu są informacje skąd użytkownicy do niego przyszli\n")
	fmt.Fprintf(os.Stderr, "    i gdzie z niego poszli (max 20 największych źródeł i ujść)\n")
	fmt.Fprintf(os.Stderr, "    Konieczne jest wcześniejsze wywołanie 'extract', a w katalogu $DATA_DIR/cache/clickstream muszą być katalogi z danymi clickstream (patrz kontener 'wiki-extra-download')\n")
	fmt.Fprintf(os.Stderr, "  clickstream doc\n")
	fmt.Fprintf(os.Stderr, "    Dokumentacja formatu generowanego przez 'clickstream'\n")
	fmt.Fprintf(os.Stderr, "  hours-pageview [N]\n")
	fmt.Fprintf(os.Stderr, "    Wykonuje kolejno czynności:\n")
	fmt.Fprintf(os.Stderr, "    - dociąga N (domyślnie 26) ostatnich plików typu 'https://dumps.wikimedia.org/other/pageviews/2019/2019-05/pageviews-20190505-120000.gz' do $DATA_DIR/cache/pageview/hours/\n")
	fmt.Fprintf(os.Stderr, "    - dla każdej wersji językowej XX Wikipedii z $DATA_DIR/cache/dumps/wikipedia/XX/current generuje plik $DATA_DIR/public/wikipedia/XX/current/hour_pageview.json.\n")
	fmt.Fprintf(os.Stderr, "      Zawiera on tablicę JSON nieposortowanych rekordów o ponad 1000 najpopularniejszych stron odwiedzanych w ciągu ostatnich 24 godzin. Format jest podzbiorem wyniku polecenia 'synthesis'\n")
	fmt.Fprintf(os.Stderr, "      z dokładnością do pola 'views', które ma 5 wartości - odpowiednio liczba odwiedzin strony w ciągu ostatnich 1,3,6,12,24 godzin\n")
	fmt.Fprintf(os.Stderr, "    Konieczne jest wcześniejsze wywołanie 'extract' i 'clickstream'.\n")
	fmt.Fprintf(os.Stderr, "    Takie narzędzie powinno być zapuszczane średnio raz na godzinę.\n")
	fmt.Fprintf(os.Stderr, "  createts enwiki\n")
	fmt.Fprintf(os.Stderr, "    Tworzy lub aktualizuje plik $DATA_DIR/wikipedia/createts/en.tsv z rekordami postaci <page_id> <create_ts> dla każdego artykułu z $DATA_DIR/wikipedia/en/articles.tsv.gz\n")
	fmt.Fprintf(os.Stderr, "    Konieczne jest wcześniejsze wywołanie 'extract'.\n")
	fmt.Fprintf(os.Stderr, "  monitor enwiki -with-subcats=1 -remove-cats='Ethereum|382943' -timespan '2019-06-01:2019-06-10' 'Cryptocurrencies'\n")
	fmt.Fprintf(os.Stderr, "    Generuje w $DATA_DIR/public pliki 'categories.json.gz' oraz 'stats.tsv.gz'. Rekord pliku drugiego ma postać 'page_id,create_date,last_mod_date,popularity_in_2018,cat_ids,pageviews'\n")
	fmt.Fprintf(os.Stderr, "    Konieczne jest wcześniejsze wywołanie 'extract', 'cathier', 'createts' i 'clickstream'. Ponadto trzeba wykonać kontener 'wiki-extra-download year-pageview' i 'wiki-extra-download clickstream'\n")
	fmt.Fprintf(os.Stderr, "  monitor doc\n")
	fmt.Fprintf(os.Stderr, "    Dokumentacja opcji polecenia 'monitor'\n")
	os.Exit(1)
}

// Writer ...
var Writer io.WriteCloser

// RecordCounter ...
var RecordCounter int = 0

// Buffer ...
var Buffer []string = make([]string, 0)

func sql2tsv(p map[string]string) {
	RecordCounter++
	if RecordCounter == 1 {
		fmt.Fprintf(Writer, "%s\n", strings.Join(utils.PGSColumnNames, "\t"))
	} else {
		for _, colName := range utils.PGSColumnNames {
			Buffer = append(Buffer, p[colName])
		}
		fmt.Fprintf(Writer, "%s\n", strings.Join(Buffer, "\t"))
		Buffer = Buffer[:0]
	}
}

func parseDumpDateStr() {
	if len(os.Args) == 0 {
		usage()
	}
	var dumpDateStr string = "current"
	r, _ := regexp.Compile("^-?-dump(=|$)")
	s := r.FindString(os.Args[0])
	if len(s) > 0 {
		index := strings.Index(s, "=")
		var value string
		if index > 0 {
			value = strings.TrimSpace(os.Args[0][index+1:])
			os.Args = os.Args[1:]
		} else if len(os.Args) >= 2 {
			value = strings.TrimSpace(os.Args[1])
			os.Args = os.Args[2:]
		} else {
			usage()
		}
		if value != "current" {
			r, _ := regexp.Compile("\\D")
			dumpDateStr = r.ReplaceAllString(value, "") // zamieniamy "2019-12-20" na "20191220"
			r, _ = regexp.Compile("^[0-9]{8}$")
			if !r.MatchString(dumpDateStr) {
				usage()
			}
		}
		utils.DumpDateStr = dumpDateStr
	}
}

func main() {
	os.Args = os.Args[1:]
	parseDumpDateStr()
	if len(os.Args) == 0 {
		usage()
	}
	var res bool
	cmd := os.Args[0]
	if cmd == "extract" {
		res = extract.Main(os.Args)
	} else if cmd == "sql2tsv" {
		Writer = gzip.NewWriter(os.Stdout)
		defer Writer.Close()
		utils.ProcessGzippedStream("stdin", os.Stdin, sql2tsv)
		res = true
	} else if cmd == "hours-pageview" {
		res = pageview_hour.Main(os.Args)
	} else if cmd == "months-pageview" {
		res = pageview.Main(os.Args)
	} else if cmd == "cathier" {
		res = cathier.Main(os.Args)
	} else if cmd == "synthesis" {
		res = synthesis.Main(os.Args)
	} else if cmd == "geomap" {
		res = geomap.Main(os.Args)
	} else if cmd == "clickstream" {
		res = clickstream.Main(os.Args)
	} else if cmd == "monitor" {
		res = monitor.Main(os.Args)
	} else if cmd == "createts" {
		res = createts.Main(os.Args)
	} else if cmd == "extra-download" {
		res = extra_download.Main(os.Args)
	} else if cmd == "events" {
		res = events.Main(os.Args)
	}
	if !res {
		usage()
	}
}
