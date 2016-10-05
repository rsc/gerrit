// Copyright 2016 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO: http 429 means slow down

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/build/gerrit"

	"rsc.io/dbstore"
	_ "rsc.io/sqlite"
)

// TODO: pragma journal_mode=WAL

// Database tables. DO NOT CHANGE.

type ProjectSync struct {
	Host string `dbstore:",key"` // "go-review.googlesource.com"
	Date string
}

type RawJSON struct {
	Host         string `dbstore:",key"`
	Number       int64  `dbstore:",key"`
	ID           string
	ChangeInfo   []byte `dbstore:",blob"`
	Comments     []byte `dbstore:",blob"`
	NeedComments bool
	NeedIndex    bool
}

type History struct {
	RowID  int64 `dbstore:",rowid"`
	Host   string
	Number int64
	Time   string
	Who    string
	Action string
	Text   string
}

var (
	file    = flag.String("f", os.Getenv("HOME")+"/gerritreview.db", "database `file` to use")
	storage = new(dbstore.Storage)
	db      *sql.DB
)

func usage() {
	fmt.Fprintf(os.Stderr, `usage: reviewdb [-f db] command [args]

Commands are:

	init (initialize new database)
	add <host> (add new repository)
	sync (sync repositories)

The default database is $HOME/gerritreview.db.
`)
	os.Exit(2)
}

func main() {
	log.SetPrefix("reviewdb: ")
	log.SetFlags(0)

	storage.Register(new(ProjectSync))
	storage.Register(new(RawJSON))
	storage.Register(new(History))

	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	if args[0] == "init" {
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "usage: reviewdb [-f db] init\n")
			os.Exit(2)
		}
		_, err := os.Stat(*file)
		if err == nil {
			log.Fatalf("creating database: file %s already exists", *file)
		}
		db, err := sql.Open("sqlite3", *file)
		if err != nil {
			log.Fatalf("creating database: %v", err)
		}
		defer db.Close()
		rows, err := db.Query("pragma journal_mode=wal")
		if err != nil {
			log.Fatal(err)
		}
		rows.Close()
		if err := storage.CreateTables(db); err != nil {
			log.Fatalf("initializing database: %v", err)
		}
		return
	}

	_, err := os.Stat(*file)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	db, err = sql.Open("sqlite3", *file)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer db.Close()
	
	db.Exec("pragma busy_timeout = 1000")

	// TODO: Remove or deal with better.
	// This is here so that if we add new tables they get created in old databases.
	// But there is nothing to recreate or expand tables in old databases.

	switch args[0] {
	default:
		usage()

	case "add":
		if len(args) != 2 {
			fmt.Fprintf(os.Stderr, "usage: issuedb [-f db] add host\n")
			os.Exit(2)
		}
		var proj ProjectSync
		proj.Host = args[1]
		if err := storage.Read(db, &proj); err == nil {
			log.Fatalf("host %s already stored in database", proj.Host)
		}

		proj.Host = args[1]
		if err := storage.Insert(db, &proj); err != nil {
			log.Fatalf("adding project: %v", err)
		}
		return

	case "sync":
		var projects []ProjectSync
		if err := storage.Select(db, &projects, ""); err != nil {
			log.Fatalf("reading projects: %v", err)
		}
		for _, proj := range projects {
			doSync(&proj)
		}

	case "refill":
		host := "go-review.googlesource.com"
		if len(args) > 1 {
			host = args[1]
		}
		refill(host)

	case "dash":
		host := "go-review.googlesource.com"
		if len(args) > 1 {
			host = args[1]
		}
		minDate := dashMinDate
		if len(args) > 2 {
			minDate = args[2]
		}
		dash(host, minDate)
	}
}

func doSync(proj *ProjectSync) {
	syncChangeInfo(proj)
	syncComments(proj)
}

func syncChangeInfo(proj *ProjectSync) {
	query := "after:1970-01-01"
	if proj.Date != "" {
		query = `after:"` + proj.Date + `"`
	}

	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()

	var recent string
	const N = 1000
	for start := 0; ; {
		values := url.Values{
			"q": {query},
			"o": {
				"ALL_REVISIONS",
				"DETAILED_ACCOUNTS",
				"DETAILED_LABELS",
				"ALL_COMMITS",
				"ALL_FILES",
				"MESSAGES",
			},
			"n":     {fmt.Sprint(N)},
			"start": {fmt.Sprint(start)},
		}

	Again:
		urlStr := "https://" + proj.Host + "/changes/?" + values.Encode()
		resp, err := http.Get(urlStr)
		println("URL:", urlStr)
		if err != nil {
			log.Fatal(err)
		}
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 429 {
			time.Sleep(1 * time.Minute)
			goto Again
		}
		if resp.StatusCode != 200 {
			log.Fatalf("%s\n%s", resp.Status, data)
		}
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			log.Fatalf("json too short: %s", data)
		}
		data = data[i:]

		var all []json.RawMessage
		if err := json.Unmarshal(data, &all); err != nil {
			log.Fatalf("parsing body: %v", err)
		}
		println("GOT", len(all), "messages")

		var more bool
		for _, m := range all {
			var meta struct {
				ID      string `json:"id"`
				Number  int64  `json:"_number"`
				More    bool   `json:"_more_changes"`
				Updated string `json:"updated"`
			}
			if err := json.Unmarshal(m, &meta); err != nil {
				log.Fatalf("parsing entry: %v\n%s", err, m)
			}
			if meta.ID == "" || meta.Number == 0 {
				log.Fatalf("parsing entry: missing ID or change number:\n%s", m)
			}
			if recent < meta.Updated {
				recent = meta.Updated
			}
			println("META:", meta.Number, meta.ID, meta.Updated, meta.More)
			more = meta.More
			var raw RawJSON
			raw.Host = proj.Host
			raw.ID = meta.ID
			raw.Number = meta.Number
			raw.ChangeInfo = m
			raw.NeedComments = true
			raw.NeedIndex = true
			if err := storage.Insert(tx, &raw); err != nil {
				log.Fatal(err)
			}
		}
		start += len(all)
		if !more {
			break
		}
	}
	if recent != "" {
		proj.Date = recent
		if err := storage.Write(tx, proj, "Date"); err != nil {
			log.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}

func syncComments(proj *ProjectSync) {
	rows, err := db.Query("select Number from RawJSON where Host == ? and NeedComments == ?", proj.Host, true)
	if err != nil {
		log.Fatal(err)
	}
	var numbers []int64
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			log.Fatal(err)
		}
		numbers = append(numbers, x)
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
	rows.Close()

	for _, x := range numbers {
		syncComment(proj, x)
	}
}

func syncComment(proj *ProjectSync, number int64) {
	urlStr := "https://" + proj.Host + "/changes/" + fmt.Sprint(number) + "/comments"
Again:
	resp, err := http.Get(urlStr)
	println("URL:", urlStr)
	if err != nil {
		log.Fatalf("fetching %s: %v", urlStr, err)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("reading %s: %v", urlStr, err)
	}
	resp.Body.Close()
	if resp.StatusCode == 429 {
		println("SLEEP for", urlStr, time.Now().Format(time.Stamp))
		time.Sleep(1 * time.Minute)
		goto Again
	}
	if resp.StatusCode != 200 {
		if resp.StatusCode == 404 {
			var raw RawJSON
			raw.Host = proj.Host
			raw.Number = number
			raw.NeedComments = false
			if err := storage.Write(db, &raw, "Comments", "NeedComments"); err != nil {
				log.Fatal(err)
			}
			return
		}			
		log.Fatalf("fetching %s: %s\n%s", urlStr, resp.Status, data)
	}
	i := bytes.IndexByte(data, '\n')
	if i < 0 {
		log.Fatalf("json too short: %s", data)
	}
	data = data[i:]

	var js json.RawMessage
	if err := json.Unmarshal(data, &js); err != nil {
		log.Fatalf("parsing body: %v", err)
	}

	var raw RawJSON
	raw.Host = proj.Host
	raw.Number = number
	raw.NeedComments = false
	raw.Comments = js
	if err := storage.Write(db, &raw, "Comments", "NeedComments"); err != nil {
		log.Fatal(err)
	}
}

func js(x interface{}) string {
	data, err := json.MarshalIndent(x, "", "\t")
	if err != nil {
		return "ERROR: " + err.Error()
	}
	return string(data)
}

func refill(host string) {
	if _, err := db.Exec("delete from History where Host = ?", host); err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec("update RawJSON set NeedIndex = ? where Host = ?", true, host); err != nil {
		log.Fatal(err)
	}
	for {
		var all []RawJSON
		if err := storage.Select(db, &all, "where Host = ? and NeedIndex == ? order by Number limit 100", host, true); err != nil {
			log.Fatalf("sql: %v", err)
		}
		if len(all) == 0 {
			break
		}
		println("GOT", len(all), all[0].Number, all[len(all)-1].Number)
		tx, err := db.Begin()
		if err != nil {
			log.Fatal(err)
		}
		for _, m := range all {
			var ch gerrit.ChangeInfo
			if err := json.Unmarshal(m.ChangeInfo, &ch); err != nil {
				log.Printf("unmarshal: %v\n%s", err, m.ChangeInfo)
				continue
			}
			if ch.Project == "scratch" {
				continue
			}
			var h History
			h.Host = m.Host
			h.Number = m.Number
			h.Time = ch.Created.Time().UTC().Format(time.RFC3339)
			h.Who = ch.Owner.Email
			h.Action = "create"
			h.Text = ch.Subject
			if err := storage.Insert(tx, &h); err != nil {
				log.Fatal(err)
			}
			h.RowID = 0
			hstart := h
			sawAbandon := false
			for _, m := range ch.Messages {
				h.Time = m.Time.Time().UTC().Format(time.RFC3339)
				if m.Author == nil {
					h.Who = "Gerrit"
				} else {
					h.Who = m.Author.Email
				}
				h.Text = m.Message
				if strings.HasPrefix(h.Text, "Uploaded") || strings.HasSuffix(h.Text, ": Commit message was updated.") {
					h.Action = "upload"
					for _, rev := range ch.Revisions {
						if rev.PatchSetNumber == m.RevisionNumber {
							h.Text += "\n" + rev.Commit.Message
						}
					}
				} else if h.Who == ch.Owner.Email {
					h.Action = "reply"
				} else {
					h.Action = "comment"
				}
				if err := storage.Insert(tx, &h); err != nil {
					log.Fatal(err)
				}
				if strings.HasPrefix(h.Text, "Abandoned") {
					sawAbandon = true
				}
				h.RowID = 0
			}
			if ch.Status == "ABANDONED" && !sawAbandon {
				h = hstart
				h.Action = "abandon"
				h.Text = ""
				h.Time = ch.Updated.Time().UTC().Format(time.RFC3339)
				if err := storage.Insert(tx, &h); err != nil {
					log.Fatal(err)
				}
				h.RowID = 0
			}
			if ch.Status == "MERGED" {
				rev := ch.Revisions[ch.CurrentRevision]
				h.Action = "merge"
				h.Who = rev.Commit.Committer.Email
				h.Time = rev.Commit.Committer.Date.Time().UTC().Format(time.RFC3339)
				h.Text = rev.Commit.Message
				if err := storage.Insert(tx, &h); err != nil {
					log.Fatal(err)
				}
				h.RowID = 0
			}

			m.NeedIndex = false
			if err := storage.Write(tx, &m, "NeedIndex"); err != nil {
				log.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			log.Fatal(err)
		}
	}
}
