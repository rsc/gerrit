// Copyright 2013 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Originally code.google.com/p/rsc/cmd/issue/acme.go.

package main

import (
	"bytes"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"9fans.net/go/acme"
	"9fans.net/go/draw"
)

var all struct {
	sync.Mutex
	m      map[string]*awin
	f      map[string]*draw.Font
	numwin int
}

func (w *awin) exit() {
	all.Lock()
	defer all.Unlock()
	if all.m[w.title] == w {
		delete(all.m, w.title)
	}
	if all.numwin--; all.numwin == 0 {
		os.Exit(0)
	}
}

func (w *awin) new(title string) *awin {
	all.Lock()
	defer all.Unlock()
	all.numwin++
	if all.m == nil {
		all.m = make(map[string]*awin)
	}
	w1 := new(awin)
	w1.title = title
	var err error
	w1.Win, err = acme.New()
	if err != nil {
		log.Printf("creating acme window: %v", err)
		time.Sleep(10 * time.Millisecond)
		w1.Win, err = acme.New()
		if err != nil {
			log.Fatalf("creating acme window again: %v", err)
		}
	}
	w1.prefix = w.prefix
	w1.Name(w1.prefix + title)
	if title != "new" {
		all.m[title] = w1
	}
	return w1
}

func (w *awin) show(title string) *awin {
	all.Lock()
	defer all.Unlock()
	if w1 := all.m[title]; w1 != nil {
		w.Ctl("show")
		return w1
	}
	return nil
}

func (w *awin) fixfont() {
	ctl := make([]byte, 1000)
	w.Seek("ctl", 0, 0)
	n, err := w.Read("ctl", ctl)
	if err != nil {
		return
	}
	f := strings.Fields(string(ctl[:n]))
	if len(f) < 8 {
		return
	}
	w.tab, _ = strconv.Atoi(f[7])
	if w.tab == 0 {
		return
	}
	name := f[6]
	if w.fontName == name {
		return
	}
	all.Lock()
	defer all.Unlock()
	if font := all.f[name]; font != nil {
		w.font = font
		w.fontName = name
		return
	}
	var disp *draw.Display = nil
	font, err := disp.OpenFont(name)
	if err != nil {
		return
	}
	if all.f == nil {
		all.f = make(map[string]*draw.Font)
	}
	all.f[name] = font
	w.font = font
}

func (w *awin) blinker() func() {
	c := make(chan struct{})
	go func() {
		t := time.NewTicker(1000 * time.Millisecond)
		defer t.Stop()
		dirty := false
		for {
			select {
			case <-t.C:
				dirty = !dirty
				if dirty {
					w.Ctl("dirty")
				} else {
					w.Ctl("clean")
				}
			case <-c:
				if dirty {
					w.Ctl("clean")
				}
				c <- struct{}{}
				return
			}
		}
	}()
	return func() {
		c <- struct{}{}
		<-c
	}
}

func (w *awin) clear() {
	w.Addr(",")
	w.Write("data", nil)
}

func (w *awin) err(s string) {
	if !strings.HasSuffix(s, "\n") {
		s = s + "\n"
	}
	w1 := w.show("+Errors")
	if w1 == nil {
		w1 = w.new("+Errors")
		w1.mode = modeErrors
		go w1.loop()
	}
	w1.Fprintf("body", "%s", s)
	w1.Addr("$")
	w1.Ctl("dot=addr")
	w1.Ctl("show")
}

func (w *awin) loadText(e *acme.Event) {
	if len(e.Text) == 0 && e.Q0 < e.Q1 {
		w.Addr("#%d,#%d", e.Q0, e.Q1)
		data, err := w.ReadAll("xdata")
		if err != nil {
			w.err(err.Error())
		}
		e.Text = data
	}
}

func (w *awin) selection() string {
	w.Ctl("addr=dot")
	data, err := w.ReadAll("xdata")
	if err != nil {
		w.err(err.Error())
	}
	return string(data)
}

func (w *awin) sort() {
	if err := w.Addr("0/^[0-9]/,"); err != nil {
		w.err("nothing to sort")
	}
	data, err := w.ReadAll("xdata")
	if err != nil {
		w.err(err.Error())
		return
	}
	suffix := ""
	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] == "" {
		suffix = "\n"
		lines = lines[:len(lines)-1]
	}
	if w.sortByNumber {
		sort.Stable(byNumber(lines))
	} else {
		sort.Stable(bySecondField(lines))
	}
	w.Addr("0/^[0-9]/,")
	w.Write("data", []byte(strings.Join(lines, "\n")+suffix))
	w.Addr("0")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

type byNumber []string

func (x byNumber) Len() int      { return len(x) }
func (x byNumber) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x byNumber) Less(i, j int) bool {
	return lineNumber(x[i]) > lineNumber(x[j])
}

func lineNumber(s string) int {
	n := 0
	for j := 0; j < len(s) && '0' <= s[j] && s[j] <= '9'; j++ {
		n = n*10 + int(s[j]-'0')
	}
	return n
}

type bySecondField []string

func (x bySecondField) Len() int      { return len(x) }
func (x bySecondField) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x bySecondField) Less(i, j int) bool {
	return skipField(x[i]) < skipField(x[j])
}

func skipField(s string) string {
	i := strings.Index(s, "\t")
	if i < 0 {
		return s
	}
	for i < len(s) && s[i+1] == '\t' {
		i++
	}
	return s[i:]
}

func (w *awin) printTabbed(text string) {
	lines := strings.SplitAfter(text, "\n")
	var allRows [][]string
	for _, line := range lines {
		if line == "" {
			continue
		}
		line = strings.TrimSuffix(line, "\n")
		allRows = append(allRows, strings.Split(line, "\t"))
	}

	var buf bytes.Buffer
	for len(allRows) > 0 {
		if row := allRows[0]; len(row) <= 1 {
			if len(row) > 0 {
				buf.WriteString(row[0])
			}
			buf.WriteString("\n")
			allRows = allRows[1:]
			continue
		}

		i := 0
		for i < len(allRows) && len(allRows[i]) > 1 {
			i++
		}

		rows := allRows[:i]
		allRows = allRows[i:]

		var wid []int

		if w.font != nil {
			for _, row := range rows {
				for len(wid) < len(row) {
					wid = append(wid, 0)
				}
				for i, col := range row {
					n := w.font.StringWidth(col)
					if wid[i] < n {
						wid[i] = n
					}
				}
			}
		}

		for _, row := range rows {
			for i, col := range row {
				buf.WriteString(col)
				if i == len(row)-1 {
					break
				}
				if w.font == nil || w.tab == 0 {
					buf.WriteString("\t")
					continue
				}
				pos := w.font.StringWidth(col)
				for pos <= wid[i] {
					buf.WriteString("\t")
					pos += w.tab - pos%w.tab
				}
			}
			buf.WriteString("\n")
		}
	}

	w.Write("body", buf.Bytes())
}

func diff(line, field, old string) *string {
	old = strings.TrimSpace(old)
	line = strings.TrimSpace(strings.TrimPrefix(line, field))
	if old == line {
		return nil
	}
	return &line
}
