// Copyright 2013 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Originally code.google.com/p/rsc/cmd/issue/acme.go.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"9fans.net/go/acme"
	"9fans.net/go/draw"
)

func acmeMode() {
	var dummy awin
	dummy.prefix = "/gerrit/go/" // XXX
	if flag.NArg() > 0 {
		// TODO(rsc): Without -a flag, the query is concatenated into one query.
		// Decide which behavior should be used, and use it consistently.
		// TODO(rsc): Block this look from doing the multiline selection mode?
		for _, arg := range flag.Args() {
			if dummy.look(arg) {
				continue
			}
			dummy.newSearch("search", arg)
		}
	} else {
		dummy.look("all")
	}
	select {}
}

const (
	modeQuery = 1 + iota
	modeCL
	modePatchSet
	modeErrors
)

type awin struct {
	*acme.Win
	prefix   string
	tab      int
	font     *draw.Font
	fontName string

	sortByNumber bool // otherwise sort by title
	mode         int
	query        string
	title        string
	cl           *CL
	changeNumber int
	basePatchSet int
	patchSet     int
}

var (
	numRE      = regexp.MustCompile(`(?m)^([0-9]{4,})(\.[0-9]+)?(\.[0-9]+)?\t`)
	patchSetRE = regexp.MustCompile(`(?m)^([0-9]{4,})(\.[0-9]+)?(\.[0-9]+)?$`)
)

func (w *awin) look(text string) bool {
	ids := readBulkIDs([]byte(text))
	if len(ids) > 0 {
		for _, id := range ids {
			if w.show(id) != nil {
				continue
			}
			w.newCL(id)
		}
		return true
	}

	if text == "all" {
		if w.show("all") != nil {
			return true
		}
		w.newSearch("all", "")
		return true
	}

	if m := patchSetRE.FindStringSubmatch(text); m != nil {
		if w.show(text) != nil {
			return true
		}
		w.newCL(text)
		return true
	}

	if m := numRE.FindAllString(text, -1); m != nil {
		for _, s := range m {
			w.look(s)
		}
		return true
	}
	return false
}

func (w *awin) newCL(name string) {
	w = w.new(name)
	w.mode = modeCL
	m := patchSetRE.FindStringSubmatch(name)
	switch {
	case len(m) == 0:
		println("BAD", name)
	case m[3] != "":
		w.changeNumber, _ = strconv.Atoi(m[1])
		w.basePatchSet, _ = strconv.Atoi(m[2][1:])
		w.patchSet, _ = strconv.Atoi(m[3][1:])
		w.mode = modePatchSet
	case m[2] != "":
		w.changeNumber, _ = strconv.Atoi(m[1])
		w.patchSet, _ = strconv.Atoi(m[2][1:])
		w.mode = modePatchSet
	default:
		w.changeNumber, _ = strconv.Atoi(m[1])
	}
	w.Ctl("cleartag")
	w.Fprintf("tag", " Get Put Look ")
	go w.load()
	go w.loop()
}

func (w *awin) newSearch(title, query string) {
	w = w.new(title)
	w.mode = modeQuery
	w.query = query
	w.Ctl("cleartag")
	w.Fprintf("tag", " New Get Sort Search ")
	w.Write("body", []byte("Loading..."))
	go w.load()
	go w.loop()
}

func readBulkIDs(text []byte) []string {
	var ids []string
	for _, line := range strings.Split(string(text), "\n") {
		if i := strings.Index(line, "\t"); i >= 0 {
			line = line[:i]
		}
		if i := strings.Index(line, " "); i >= 0 {
			line = line[:i]
		}
		if patchSetRE.MatchString(line) {
			ids = append(ids, line)
		}
	}
	return ids
}

func (w *awin) load() {
	w.fixfont()

	switch w.mode {
	case modeQuery:
		var buf bytes.Buffer
		stop := w.blinker()
		err := showQuery(&buf, w.query)
		stop()
		w.clear()
		if err != nil {
			w.Write("body", []byte(err.Error()))
			break
		}
		if w.title == "search" {
			w.Fprintf("body", "Search %s\n\n", w.query)
		}
		w.printTabbed(buf.String())
		w.Ctl("clean")

	case modeCL:
		var buf bytes.Buffer
		stop := w.blinker()
		cl, err := showCL(&buf, w.changeNumber)
		stop()
		w.clear()
		if err != nil {
			w.Write("body", []byte(err.Error()))
			break
		}
		w.Write("body", buf.Bytes())
		w.Ctl("clean")
		w.cl = cl

	case modePatchSet:
		var buf bytes.Buffer
		stop := w.blinker()
		cl, err := showPatchSet(&buf, w.changeNumber, w.basePatchSet, w.patchSet)
		stop()
		w.clear()
		if err != nil {
			w.Write("body", []byte(err.Error()))
			break
		}
		w.Write("body", buf.Bytes())
		w.Ctl("clean")
		w.cl = cl

	}

	w.Addr("0")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

func (w *awin) put() {
	stop := w.blinker()
	defer stop()
	switch w.mode {
	case modeCL, modePatchSet:
		data, err := w.ReadAll("body")
		if err != nil {
			w.err(fmt.Sprintf("Put: %v", err))
			return
		}
		if w.mode == modeCL {
			err = writeCL(w.cl, data)
		} else {
			err = writePatchSet(w.cl, data)
		}
		if err != nil {
			w.err(err.Error())
			return
		}
		w.load()

	case modeQuery:
		w.err("cannot Put list")
	}
}

func (w *awin) submit() {
	if *flagN {
		w.err("submit")
		return
	}
	stop := w.blinker()
	err := client.Submit(w.cl.ChangeInfo.ID)
	stop()
	if err != nil {
		w.err(fmt.Sprintf("Submit: %v", err))
		return
	}
	w.load()
}

func (w *awin) abandon() {
	if *flagN {
		w.err("abandon")
		return
	}
	stop := w.blinker()
	err := client.Abandon(w.cl.ChangeInfo.ID)
	stop()
	if err != nil {
		w.err(fmt.Sprintf("Abandon: %v", err))
		return
	}
	w.load()
}

func (w *awin) loop() {
	defer w.exit()
	for e := range w.EventChan() {
		switch e.C2 {
		case 'x', 'X': // execute
			cmd := strings.TrimSpace(string(e.Text))
			if cmd == "Get" {
				w.load()
				break
			}
			if cmd == "Put" {
				w.put()
				break
			}
			if cmd == "Del" {
				w.Ctl("del")
				break
			}
			if cmd == "Submit" {
				if w.mode != modeCL {
					w.err("can only submit top-level CL")
					break
				}
				w.submit()
				break
			}
			if cmd == "Nop" {
				*flagN = !*flagN
				w.err(fmt.Sprintf("flagN = %v\n", *flagN))
				break
			}
			if cmd == "Abandon" {
				if w.mode != modeCL {
					w.err("can only abandon top-level CL")
					break
				}
				w.abandon()
				break
			}
			if cmd == "Sort" {
				if w.mode != modeQuery {
					w.err("can only sort list windows")
					break
				}
				w.sortByNumber = !w.sortByNumber
				w.sort()
				break
			}
			if strings.HasPrefix(cmd, "Search ") {
				w.newSearch("search", strings.TrimSpace(strings.TrimPrefix(cmd, "Search")))
				break
			}
			w.WriteEvent(e)
		case 'l', 'L': // look
			// TODO(rsc): Expand selection, especially for URLs.
			w.loadText(e)
			if !w.look(string(e.Text)) {
				w.WriteEvent(e)
			}
		}
	}
}
