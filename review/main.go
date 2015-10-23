// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO: Cache loaded information except on Get.
// TODO: Expand clicks like on 1234.4
// TODO: Set up plumbing rules for issues.
// TODO: Some kind of config file [sic]?

// TODO: Writing comments.
// TODO: Show drafts.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"

	"rsc.io/gerrit/internal/gerrit"
)

var client *gerrit.Client

var flagA = flag.Bool("a", false, "acme mode")
var flagN = flag.Bool("n", false, "print but do not execute Gerrit write operations")

func main() {
	flag.Parse()

	client = gerrit.NewClient("https://go-review.googlesource.com", loadAuth("go-review.googlesource.com"))

	if *flagA {
		acmeMode()
		return
	}

	/*
		chs, err := client.QueryChanges("is:open -project:scratch -message:do-not-review reviewer:rsc", gerrit.QueryChangesOpt{})
		if err != nil {
			log.Fatal(err)
		}

		for _, ch := range chs {
			fmt.Printf("%d\t%s\n", ch.ChangeNumber, ch.Subject)
		}
	*/

	//showCL(os.Stdout, 13975)
	//return

	ch, err := client.GetChangeDetail(flag.Arg(0), gerrit.QueryChangesOpt{
		Fields: []string{
			"ALL_REVISIONS",
			"DETAILED_ACCOUNTS",
			"DETAILED_LABELS",
			"ALL_COMMITS",
			"ALL_FILES",
			"MESSAGES",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if true {
		fmt.Printf("%s\n", js(ch))
		return
	}

	if false {
		fmt.Printf("query: %s\n", flag.Arg(0))
		acct, err := client.SuggestReviewers(ch.ID, flag.Arg(0), 10)
		if err != nil {
			log.Fatal(err)
		}
		js, err := json.MarshalIndent(acct, "", "\t")
		if err != nil {
			log.Print(err)
			return
		}
		fmt.Printf("%s\n", js)
		return
	}

	showPatchSet(os.Stdout, ch.ChangeNumber, 0, 2)
	return

	revID := ch.CurrentRevision
	rev := ch.Revisions[revID]

	var files []string
	for file := range rev.Files {
		files = append(files, file)
	}
	sort.Strings(files)

	for _, filePath := range files {
		diff, err := client.GetDiff(ch.ID, revID, filePath, gerrit.GetDiffOpt{
			// We use the full file context even to prepare shorter diff views.
			// The Gerrit server seems to send full context no matter what,
			// so this line is not strictly necessary, but in case that apparent
			// bug gets fixed, ask for full context explicitly.
			Context: -1,
		})
		if err != nil {
			log.Print(err)
			continue
		}

		const maxContext = 3
		fmt.Printf("File %s\n\n", filePath)
		for _, line := range diff.DiffHeader {
			fmt.Printf("\t%s\n", line)
		}
		for _, c := range diff.Content {
			if c.AB != nil {
				if len(c.AB) > 2*maxContext+3 {
					for _, line := range c.AB[:maxContext] {
						fmt.Printf("\t %s\n", line)
					}
					fmt.Printf("\t...\n")
					for _, line := range c.AB[len(c.AB)-maxContext:] {
						fmt.Printf("\t %s\n", line)
					}
				} else {
					for _, line := range c.AB {
						fmt.Printf("\t %s\n", line)
					}
				}
			} else {
				for _, line := range c.A {
					fmt.Printf("\t-%s\n", line)
				}
				for _, line := range c.B {
					fmt.Printf("\t+%s\n", line)
				}
			}
		}
		fmt.Printf("\n")
		if true {
			js, err := json.MarshalIndent(diff, "", "\t")
			if err != nil {
				log.Print(err)
				continue
			}
			fmt.Printf("%s %s\n%s\n", revID, filePath, js)
		}
	}
	return
}

func loadAuth(host string) gerrit.Auth {
	// First look in Git's http.cookiefile, which is where Gerrit
	// now tells users to store this information.
	if cookieFile, _ := trimErr(cmdOutputDirErr(".", "git", "config", "http.cookiefile")); cookieFile != "" {
		data, _ := ioutil.ReadFile(cookieFile)
		maxMatch := -1
		var cookieName, cookieValue string
		for _, line := range lines(string(data)) {
			f := strings.Split(line, "\t")
			if len(f) >= 7 && (f[0] == host || strings.HasPrefix(f[0], ".") && strings.HasSuffix(host, f[0])) {
				if len(f[0]) > maxMatch {
					cookieName = f[5]
					cookieValue = f[6]
					maxMatch = len(f[0])
				}
			}
		}
		if maxMatch > 0 && cookieName == "o" {
			i := strings.Index(cookieValue, "=")
			if i >= 0 {
				return gerrit.BasicAuth(cookieValue[:i], cookieValue[i+1:])
			}
		}
	}

	// If not there, then look in $HOME/.netrc, which is where Gerrit
	// used to tell users to store the information, until the passwords
	// got so long that old versions of curl couldn't handle them.
	data, _ := ioutil.ReadFile(os.Getenv("HOME") + "/.netrc")
	for _, line := range lines(string(data)) {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) >= 6 && f[0] == "machine" && f[1] == host && f[2] == "login" && f[4] == "password" {
			return gerrit.BasicAuth(f[3], f[5])
		}
	}

	return gerrit.NoAuth
}

// trim is shorthand for strings.TrimSpace.
func trim(text string) string {
	return strings.TrimSpace(text)
}

// trimErr applies strings.TrimSpace to the result of cmdOutput(Dir)Err,
// passing the error along unmodified.
func trimErr(text string, err error) (string, error) {
	return strings.TrimSpace(text), err
}

// lines returns the lines in text.
func lines(text string) []string {
	out := strings.Split(text, "\n")
	// Split will include a "" after the last line. Remove it.
	if n := len(out) - 1; n >= 0 && out[n] == "" {
		out = out[:n]
	}
	return out
}

// cmdOutputDirErr runs the command line in dir, returning its output
// and any error results.
func cmdOutputDirErr(dir, command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	if dir != "." {
		cmd.Dir = dir
	}
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func js(x interface{}) string {
	enc, err := json.MarshalIndent(x, "", "\t")
	if err != nil {
		return "ERROR: " + err.Error()
	}
	return string(enc)
}
