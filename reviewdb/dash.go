// Copyright 2016 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

type action struct {
	time   string
	op     int
	number int
	text   string
}

const (
	_ = iota
	opCreate
	opComment
	opReply
	opUpload
	opMerge
	opAbandon
)

type clState struct {
	createTime    string
	uploadTime    string
	replyTime     string
	commentTime   string
	closeTime     string
	hardCloseTime string
}

func dashActions(host string) ([]action, int) {
	var actions []action
	var maxCL int64
	var last int64
	for {
		var all []History
		if err := storage.Select(db, &all, "where Host = ? and RowID > ? order by RowID asc limit 100", host, last); err != nil {
			log.Fatalf("sql: %v", err)
		}
		if len(all) == 0 {
			break
		}
		for _, h := range all {
			if maxCL < h.Number {
				maxCL = h.Number
			}
			switch h.Action {
			case "create":
				actions = append(actions, action{h.Time, opCreate, int(h.Number), h.Who})
			case "upload":
				actions = append(actions, action{h.Time, opUpload, int(h.Number), h.Text})
			case "comment":
				actions = append(actions, action{h.Time, opComment, int(h.Number), h.Text})
			case "reply":
				actions = append(actions, action{h.Time, opReply, int(h.Number), h.Text})
			case "merge":
				actions = append(actions, action{h.Time, opMerge, int(h.Number), h.Text})
			case "abandon":
				actions = append(actions, action{h.Time, opAbandon, int(h.Number), h.Text})
			}
			last = h.RowID
		}
	}
	sort.Stable(actionsByTime(actions))
	return actions, int(maxCL)
}

type actionsByTime []action

func (x actionsByTime) Len() int           { return len(x) }
func (x actionsByTime) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }
func (x actionsByTime) Less(i, j int) bool { return x[i].time < x[j].time }

func plot(actions []action, maxCL int, emit func([]clState, string)) {
	var lastTime string
	state := make([]clState, maxCL+1)
	for _, a := range actions {
		thisTime := a.time[:10]
		if thisTime != lastTime {
			if lastTime != "" {
				emit(state, lastTime)
			}
			lastTime = thisTime
		}
		s := &state[a.number]
		switch a.op {
		case opCreate:
			s.createTime = a.time
			if a.text == "fuchsia.robot@gmail.com" {
				s.hardCloseTime = a.time
			}
		case opUpload:
			s.uploadTime = a.time
			s.closeTime = ""
			if strings.Contains(strings.ToLower(a.text), "do not review") {
				s.closeTime = a.time
			}
		case opComment, opReply:
			if a.op == opComment {
				s.commentTime = a.time
			} else {
				s.replyTime = a.time
			}
			if strings.HasPrefix(a.text, "R=") || strings.Contains(a.text, "\nR=") {
				i := 0
				if !strings.HasPrefix(a.text, "R=") {
					i = strings.Index(a.text, "\nR=") + 1
				}
				directive := a.text[i:]
				if j := strings.Index(directive, "\n"); j >= 0 {
					directive = directive[:j]
				}
				if directive == "R=close" {
					s.closeTime = a.time
				} else {
					s.closeTime = ""
				}
			}
			if strings.HasPrefix(a.text, "Abandoned") {
				s.hardCloseTime = a.time
			}
		case opMerge, opAbandon:
			s.hardCloseTime = a.time
		}
	}
	if lastTime != "" {
		emit(state, lastTime)
	}
}

const dashMinDate = "12016-04-01"

func dash(host, minDate string) {
	actions, maxCL := dashActions(host)
	plotAge(actions, maxCL, minDate)
}

func plotAge(actions []action, maxCL int, minDate string) {
	const day = 24 * time.Hour
	var cutoffs = [...]time.Duration{
		365 * day,
		180 * day,
		90 * day,
		60 * day,
		30 * day,
		14 * day,
		7 * day,
		1 * day,
		0,
	}
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "var clAgeData = [\n")
	fmt.Fprintf(&buf, "  ['Date', '\\u2265 365d', '\\u2265 180d', '\\u2265 90d', '\\u2265 60d', '\\u2265 30d', '\\u2265 14d', '\\u2265 7d', '\\u2265 1d', 'all']\n")

	plot(actions, maxCL, func(cls []clState, tm string) {
		now, err := time.Parse(time.RFC3339[:10], tm)
		if err != nil {
			log.Fatal(err)
		}
		var counts [len(cutoffs)]int
		for clnum, cl := range cls {
			if cl.createTime == "" || cl.closeTime != "" || cl.hardCloseTime != "" {
				continue
			}
			t, err := time.Parse(time.RFC3339, cl.createTime)
			if err != nil {
				log.Fatal(err)
			}
			dt := now.Sub(t)
			for i, d := range cutoffs {
				if dt >= d {
					if i == 0 {
						println("OLD", clnum)
					}
					counts[i]++
				}
			}
		}
		fmt.Fprintf(&buf, ",\n  [myDate(\"%s\")", tm)
		for _, x := range counts {
			fmt.Fprintf(&buf, ", %d", x)
		}
		fmt.Fprintf(&buf, "]")
	})
	fmt.Fprintf(&buf, "\n];\n\n")
	os.Stdout.Write(buf.Bytes())
}
