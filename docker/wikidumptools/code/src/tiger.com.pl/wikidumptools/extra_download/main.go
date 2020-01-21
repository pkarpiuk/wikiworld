package extra_download

import "fmt"
import "os"
import "path"
import "io/ioutil"
import "sync"
import "encoding/json"
import "bufio"
import "strconv"
import "time"
import "net/url"
import "net/http"
import "strings"
import "compress/gzip"
import "io"
import utils "tiger.com.pl/wikidumptools/utils"

type Record struct {
  PageId int64        `json:"page_id"`
  DownloadTs string   `json:"download_ts"`
  CreateTs string     `json:"create_ts,omitempty"`
  Summary interface{} `json:"summary,omitempty"`
  Media interface{}   `json:"media,omitempty"`
  Related []int64     `json:"related,omitempty"`
}

var TheDivider int64 = 1
var TheRest int64 = 0
var DumpsDir string
var DBDir string
var WaitGroup sync.WaitGroup
var GeoFlag bool = true // TODO: potem ustawić na false
var ThrottlingDelayMs int = 1000

func innerCountRecords( r io.Reader, result map[int64]int, negativeFlag bool ) {
  scanner := bufio.NewScanner(r)
  buf := make([]byte, 0, 10*1024*1024)
  scanner.Buffer(buf, 10*1024*1024)
  var rec map[string]interface{}
  for scanner.Scan() {
    err := json.Unmarshal(scanner.Bytes(), &rec)
    if err == nil {
      pid := int64(rec["page_id"].(float64))
      if negativeFlag {
        result[pid] = -1
      } else {
        count := result[pid]
        result[pid] = count+1
      }
    } else {
      fmt.Fprintf( os.Stderr, "ERROR[1]: %v\n", err )
    }
  }
  if err := scanner.Err(); err != nil {
    fmt.Fprintf( os.Stderr, "ERROR[2]: %v\n", err )
  }
}

func countRecords( dbFPath string, result map[int64]int ) {
  file, err := os.Open( dbFPath )
  if os.IsNotExist( err ) {
    return
  }
  if err != nil {
    panic(err)
  }
  defer file.Close()
  innerCountRecords( file, result, false )
}

func loadGeoSet( geoFPath string ) (result map[int64]bool) {
  result = make(map[int64]bool)
  utils.ProcessLocalFile( geoFPath, func(p map[string]string) {
    pid, err := strconv.ParseInt(p["gt_page_id"], 10, 64)
    if err != nil {
      fmt.Fprintf( os.Stderr, "ERROR[3]: %v\n", err )
      return
    }
    if (pid % TheDivider == TheRest) && (p["gt_primary"] == "1") {
      result[pid] = true
    }
  }, "\t", nil )
  return
}

func innerDownload( url string ) (result map[string]interface{}) {
  time.Sleep(time.Duration(ThrottlingDelayMs) * time.Millisecond)
  resp, err := http.Get(url)
  if err != nil {
    fmt.Fprintf( os.Stderr, "ERROR[5]: %v\n", err )
    return nil
  }
  defer resp.Body.Close()

  str,err := ioutil.ReadAll(resp.Body)
  if err != nil {
    panic(err)
  }
  err = json.Unmarshal( []byte(str), &result )

  // err = json.NewDecoder(resp.Body).Decode(&result)

  if err != nil {
    fmt.Fprintf( os.Stderr, "ERROR[6]: %v; %s\n  %s\n", err, url, str )
    return nil
  }
  return
}

func downloadJSON( url string ) (result map[string]interface{}) {
  // fmt.Fprintf( os.Stdout, "%s\n", url )
  counter := 0
  for {
    counter += 1
    result = innerDownload( url )
    if result != nil || counter > 3 {
      break
    }
  }
  return
}

func processLanguageInner( dbFPath string, articlesFPath string, lang string, wasPageIds map[int64]int, geoSet map[int64]bool ) int {
  newRecordsCounter := 0
  lineCounter := 0
  fout, err := os.OpenFile( dbFPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666 )
  if err != nil {
    panic( err )
  }
  defer fout.Close()
  utils.ProcessLocalFile( articlesFPath, func(p map[string]string) {
    pid, err := strconv.ParseInt(p["page_id"], 10, 64)
    if err != nil {
      fmt.Fprintf( os.Stderr, "ERROR[3]: %v\n", err )
      return
    }
    lineCounter += 1
    pageTitle := p["page_title"]
    if lineCounter % 1000 == 0 {
      fmt.Fprintf( os.Stdout, "%s %d %d %s\n", lang, lineCounter, pid, pageTitle )
    }
    if (!GeoFlag || geoSet[pid]) && (pid % TheDivider == TheRest) && (wasPageIds[pid] == 0) {
      record := Record {
        PageId: pid,
        DownloadTs: time.Now().Format("2006-01-02T15:04:05-07:00") }
      cgiTitle := url.QueryEscape( strings.Replace( pageTitle, " ", "_", -1 ) )

      js := downloadJSON( fmt.Sprintf( "https://%s.wikipedia.org/w/api.php?action=query&prop=revisions&rvlimit=1&rvprop=timestamp&rvdir=newer&format=json&formatversion=2&utf8=&pageids=%d", lang, pid ) )
      if js != nil && js["query"] != nil {
        query := js["query"].(map[string]interface{})
        if query != nil && query["pages"] != nil {
          pages := query["pages"].([]interface{})
          if pages != nil && len(pages) > 0 {
            page := pages[0].(map[string]interface{})
            if page != nil && page["revisions"] != nil {
              revisions := page["revisions"].([]interface{})
              if revisions != nil && len(revisions) > 0 {
                revision := revisions[0].(map[string]interface{})
                if revision != nil {
                  record.CreateTs = revision["timestamp"].(string)
                }
              }
            }
          }
        }
      }

      js = downloadJSON( fmt.Sprintf( "https://%s.wikipedia.org/api/rest_v1/page/summary/%s", lang, cgiTitle ) )
      if js != nil {
        delete( js, "content_urls" )
        delete( js, "api_urls" )
        record.Summary = js
      }

      js = downloadJSON( fmt.Sprintf( "https://%s.wikipedia.org/api/rest_v1/page/media/%s", lang, cgiTitle ) )
      if js != nil && js["items"] != nil {
        items := js["items"].([]interface{})
        if items != nil {
          for _, item := range items {
            itm := item.(map[string]interface{})
            delete( itm, "artist" )
            delete( itm, "credit" )
            delete( itm, "license" )
          }
          record.Media = js
        }
      }

      js = downloadJSON( fmt.Sprintf( "https://%s.wikipedia.org/api/rest_v1/page/related/%s", lang, cgiTitle ) )
      if js != nil && js["pages"] != nil {
        pages := js["pages"].([]interface{})
        if pages != nil {
          record.Related = make([]int64,0,len(pages))
          for _, page := range pages {
            pg := page.(map[string]interface{})
            if int(pg["ns"].(float64)) == 0 {
              record.Related = append(record.Related, int64(pg["pageid"].(float64)))
            }
          }
        }
      }

      data, err := json.Marshal(record)
      if err != nil {
        fmt.Fprintf( os.Stderr, "ERROR[4]: %v\n", err )
        return
      }
      _, err = fmt.Fprintln(fout, string(data))
      if err != nil {
        panic( err )
      }
      wasPageIds[pid] = 1
      newRecordsCounter += 1
    }
  }, "\t", nil )
  return newRecordsCounter
}

func makeSnapshot( lang string ) {
  dbFPath := path.Join( DBDir, lang + ".json" )
  wasPageIds := make(map[int64]int)
  supplementHash( lang, wasPageIds, false )
  countRecords( dbFPath, wasPageIds )
  if len(wasPageIds) > 0 {
    snapshotsDir := path.Join( DBDir, "snapshots" )
    snapshotFPath := path.Join( snapshotsDir, lang + ".json.gz" )
    tmpFile, err := ioutil.TempFile( snapshotsDir, "tmp-" + lang + "-" )
    if err != nil {
      panic( err )
    }
    os.Chmod( tmpFile.Name(), 0776)
    w := gzip.NewWriter(tmpFile)

    _, err = os.Stat( snapshotFPath )
    if !os.IsNotExist( err ) {
      if err != nil {
        panic(err)
      }
      sfile, err := os.Open( snapshotFPath )
      if err != nil {
        panic( err )
      }
      gz, err := gzip.NewReader(sfile)
      if err != nil {
        panic( err )
      }
      scanner := bufio.NewScanner(gz)
      buf := make([]byte, 0, 10*1024*1024)
      scanner.Buffer(buf, 10*1024*1024)
      var rec map[string]interface{}
      for scanner.Scan() {
        err := json.Unmarshal(scanner.Bytes(), &rec)
        if err == nil {
          pid := int64(rec["page_id"].(float64))
          wasPageIds[pid] -= 1
          if wasPageIds[pid] == 0 {
            data, err := json.Marshal(rec)
            if err != nil {
              panic( err )
            }
            _, err = fmt.Fprintln(w, string(data))
            if err != nil {
              panic( err )
            }
          }
        }
      }
      if err := scanner.Err(); err != nil {
        panic( err )
      }
      gz.Close()
      sfile.Close()
    }

    dbfile, err := os.Open( dbFPath )
    if err != nil {
      panic(err)
    }
    scanner := bufio.NewScanner(dbfile)
    buf := make([]byte, 0, 10*1024*1024)
    scanner.Buffer(buf, 10*1024*1024)
    var rec map[string]interface{}
    for scanner.Scan() {
      err := json.Unmarshal(scanner.Bytes(), &rec)
      if err == nil {
        pid := int64(rec["page_id"].(float64))
        wasPageIds[pid] -= 1
        if wasPageIds[pid] == 0 {
          data, err := json.Marshal(rec)
          if err != nil {
            panic( err )
          }
          _, err = fmt.Fprintln(w, string(data))
          if err != nil {
            panic( err )
          }
        }
      }
    }
    if err := scanner.Err(); err != nil {
      panic( err )
    }

    dbfile.Close()
    w.Close()
    tmpFile.Close()

    err = os.Rename( tmpFile.Name(), snapshotFPath )
    if err != nil {
      panic( err )
    }
  }
}

func supplementHash( lang string, hash map[int64]int, negativeFlag bool ) {
  snapshotsDir := path.Join( DBDir, "snapshots" )
  snapshotFPath := path.Join( snapshotsDir, lang + ".json.gz" )
  _, err := os.Stat( snapshotFPath )
  if os.IsNotExist( err ) {
    return
  }
  if err != nil {
    panic(err)
  }
  file, err := os.Open( snapshotFPath )
  if err != nil {
    panic( err )
  }
  defer file.Close()
  gz, err := gzip.NewReader(file)
  if err != nil {
    panic( err )
  }
  defer gz.Close()
  innerCountRecords( gz, hash, negativeFlag )
}

func processLanguage( dbFPath string, articlesFPath string, lang string, geoFPath string ) {
  wasPageIds := make(map[int64]int)
  countRecords( dbFPath, wasPageIds )
  cloneHash := make(map[int64]int)
  for key, val := range wasPageIds {
    cloneHash[key] = val
  }
  supplementHash( lang, wasPageIds, true ) // w wasPageIds ustawia -1 dla artykulow ze snapshota
  geoSet := make(map[int64]bool)
  if GeoFlag {
    geoSet = loadGeoSet( geoFPath )
    fmt.Fprintf( os.Stdout, "%s: geo %d\n", lang, len(geoSet) )
  }
  fmt.Fprintf( os.Stdout, "%s: was processed %d\n", lang, len(wasPageIds) )
  // W pierwszym uruchomieniu processLanguageInner dociągamy tylko te artykuły, których nie ma w ostatnim snapshocie i dbFPath
  processLanguageInner( dbFPath, articlesFPath, lang, wasPageIds, geoSet ) // w wasPageIds bedzie >0 dla artykulow spoza snapshota: tych co byly juz w dbFPath lub nowo sciagnietych
  newRecordsCount := 0
  for _, val := range wasPageIds {
    if val > 0 {
      newRecordsCount += 1
    }
  }
  // czy snapshot jest kompletny?
  if newRecordsCount == 0 { // tak; powoli aktualizujemy rekordy
    wasPageIds = cloneHash
    processLanguageInner( dbFPath, articlesFPath, lang, wasPageIds, geoSet )
  }
  makeSnapshot( lang )
  os.Remove( dbFPath )
  WaitGroup.Done()
}

func Main( args []string ) bool {
  if os.Getenv( "THROTTLING_DELAY" ) != "" {
    ThrottlingDelayMs = utils.Atoi( os.Getenv( "THROTTLING_DELAY" ) )
  }
  if len(args) == 3 {
    TheDivider = int64(utils.Atoi( args[1] ))
    TheRest = int64(utils.Atoi( args[2] ))
  }

  fmt.Fprintf( os.Stdout, "Divider: %d, Rest: %d\n", TheDivider, TheRest )

  DBDir = path.Join( utils.DataDir, "db", "wikipedia", "articles" )
  snapshotsDir := path.Join( DBDir, "snapshots" )

  // usuwamy wszystkie pliki tymczasowe z snapshotsDir (niedorobione snapshoty)
  files_list, err := ioutil.ReadDir( snapshotsDir )
  if err != nil {
    panic( err )
  }
  for _, fi := range files_list {
    if strings.HasPrefix( fi.Name(), "tmp-" ) {
      os.Remove( path.Join( snapshotsDir, fi.Name() ) )
    }
  }

  DumpsDir = path.Join( utils.DataDir, "cache", "dumps", "wikipedia" )
  files_list, err = ioutil.ReadDir( DumpsDir )
  if err != nil {
    panic( err )
  }
  for _, fi := range files_list {
    if fi.IsDir() {
      lang := fi.Name()
      dumpDir := path.Join( DumpsDir, lang, "current" )
      fi, err := os.Stat( dumpDir )
      if err == nil && fi.Mode().IsDir() {
        dbFPath := path.Join( DBDir, lang + ".json" )
        articlesFPath := path.Join( dumpDir, "articles.tsv" )
        geoFPath := path.Join( dumpDir, "geo_tags.tsv" )
        fi, err := os.Stat( articlesFPath + ".gz" )
        if err == nil && fi.Mode().IsRegular() {
          fmt.Fprintf( os.Stdout, "Processing %s...\n", lang )
          WaitGroup.Add( 1 )
          go processLanguage( dbFPath, articlesFPath, lang, geoFPath )
        }
      }
    }
  }
  WaitGroup.Wait()

  return true
}

