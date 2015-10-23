package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"rsc.io/gerrit/internal/gerrit"
)

func writeCL(old *CL, updated []byte) (xerr error) {
	var errbuf bytes.Buffer
	defer func() {
		if errbuf.Len() > 0 {
			xerr = errors.New(strings.TrimSpace(errbuf.String()))
		}
	}()

	var review gerrit.ReviewInput
	review.Labels = make(map[string]int)
	review.Drafts = "PUBLISH_ALL_REVISIONS"

	parseError := false
	off := 0
	sdata := string(updated)
	for _, origLine := range strings.SplitAfter(sdata, "\n") {
		line := strings.TrimSpace(origLine)
		if line == "" || strings.HasPrefix(line, "#") {
			off += len(origLine)
			continue
		}
		i := strings.Index(line, ":")
		if i < 0 {
			break
		}
		off += len(origLine)
		key := strings.TrimSpace(line[:i])
		value := strings.TrimSpace(line[i+1:])
		if key == "Owner" {
			continue
		}
		if key == "Reviewers" {
			have := make(map[string]string)
			for _, r := range old.Reviewers {
				have[shortEmail(r.Email)] = r.Email
				have[r.Email] = r.Email
			}
			kept := make(map[string]bool)
			kept[old.ChangeInfo.Owner.Email] = true // why the owner is a reviewer I don't know!
			for _, f := range strings.Fields(value) {
				if have[f] != "" {
					kept[have[f]] = true
					continue
				}
				q := f
				if !strings.Contains(q, "@") {
					q += "@"
				}
				if len(q) == 2 {
					q += "go"
				}
				acct, err := client.SuggestReviewers(old.ChangeInfo.ID, q, 10)
				if err != nil && len(f) >= 3 {
					acct, err = client.SuggestReviewers(old.ChangeInfo.ID, q, 10)
				}
				if err != nil || len(acct) == 0 {
					fmt.Fprintf(&errbuf, "unknown reviewer: %s\n", f)
					continue
				}
				n := 0
				var best string
				for _, r := range acct {
					if r.Account == nil {
						continue
					}
					email := r.Account.Email
					if best == "" {
						best = email
					}
					if strings.HasSuffix(email, "@golang.org") || strings.HasSuffix(email, "@google.com") {
						n++
						best = email
					}
				}
				if n > 1 || n == 0 && len(acct) > 1 {
					fmt.Fprintf(&errbuf, "ambiguous reviewer %q:", f)
					for _, r := range acct {
						if r.Account == nil {
							continue
						}
						email := r.Account.Email
						fmt.Fprintf(&errbuf, " %s", email)
					}
					fmt.Fprintf(&errbuf, "\n")
					continue
				}
				if *flagN {
					fmt.Fprintf(&errbuf, "add reviewer %s\n", best)
				} else {
					_, err = client.AddReviewer(old.ChangeInfo.ID, &gerrit.ReviewerInput{Reviewer: best})
					if err != nil {
						fmt.Fprintf(&errbuf, "adding reviewer %s: %v\n", best, err)
						continue
					}
				}
				kept[best] = true
			}
			for _, r := range old.Reviewers {
				if !kept[r.Email] {
					if *flagN {
						fmt.Fprintf(&errbuf, "delete reviewer %s\n", r.Email)
					} else {
						err := client.DeleteReviewer(old.ChangeInfo.ID, r.Email)
						if err != nil {
							fmt.Fprintf(&errbuf, "removing reviewer %s: %v\n", r.Email, err)
						}
					}
				}
			}
			continue
		}
		if _, ok := old.ChangeInfo.Labels[key]; ok {
			allowed := old.ChangeInfo.PermittedLabels[key]
			for _, vote := range strings.Fields(value) {
				for _, x := range allowed {
					if vote == strings.TrimSpace(x) {
						review.Labels[key], _ = strconv.Atoi(vote)
					}
				}
			}
			continue
		}
		fmt.Fprintf(&errbuf, "unknown summary line: %s\n", line)
		parseError = true
	}

	if parseError {
		return nil
	}

	marker := "\nPatch Set "
	var comment string
	if i := strings.Index(sdata, marker); i >= off {
		comment = strings.TrimSpace(sdata[off:i])
	}

	if comment == "<optional comment here>" {
		comment = ""
	}

	review.Message = comment

	if *flagN {
		fmt.Fprintf(&errbuf, "publish review: %s\n", js(review))
		return nil
	}

	err := client.SetReview(old.ChangeInfo.ID, old.ChangeInfo.CurrentRevision, &review)
	if err != nil {
		fmt.Fprintf(&errbuf, "error publishing review: %v\n", err)
	}

	return nil
}

var inlineCommentRE = regexp.MustCompile(`^[^ ]+ \([A-Z][a-z]{2} +[0-9]+ [0-9]+:[0-9]{2}:[0-9]{2}\):`)
var diffHunkRE = regexp.MustCompile(`^@@ -([0-9]+),([0-9]+) \+([0-9]+),([0-9]+) @@`)

func writePatchSet(old *CL, updated []byte) (xerr error) {
	var errbuf bytes.Buffer
	defer func() {
		if errbuf.Len() > 0 {
			xerr = errors.New(strings.TrimSpace(errbuf.String()))
		}
	}()

	drafts := map[string]*gerrit.CommentInfo{}
	for _, c := range old.Drafts {
		drafts[c.ID] = c
	}

	var inReplyTo *gerrit.CommentInfo
	currentFile := ""
	side := 0
	lines := strings.SplitAfter(string(updated), "\n")
	top := false
	lineNew := -1
	lineOld := -1
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if i == 0 && strings.HasPrefix(line, "CL ") {
			continue
		}
		if strings.HasPrefix(line, "File ") {
			currentFile = strings.TrimSpace(line[5:])
			lineNew = -1
			lineOld = -1
			top = true
			continue
		}
		if strings.HasPrefix(line, DiffPrefix) {
			top = false
			inReplyTo = nil
			line = strings.TrimPrefix(line, DiffPrefix)
			if m := diffHunkRE.FindStringSubmatch(line); m != nil {
				lineOld, _ = strconv.Atoi(m[1])
				lineNew, _ = strconv.Atoi(m[3])
			} else if lineNew >= 1 && lineOld >= 1 {
				if strings.HasPrefix(line, "+") {
					lineNew++
					side = +1
				} else if strings.HasPrefix(line, "-") {
					lineOld++
					side = -1
				} else {
					lineNew++
					lineOld++
					side = 0
				}
			}
			continue
		}
		if m := inlineCommentRE.FindStringSubmatch(line); m != nil {
			inReplyTo = findComment(old, m[0], currentFile, side, lineOld, lineNew)
			for i+1 < len(lines) && isCont(lines[i+1]) {
				i++
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Otherwise, we seem to have found new text.
		// Gather a draft comment.
		var c gerrit.CommentInfo

		start := i
		for i+1 < len(lines) && isDraftLine(lines[i+1]) {
			i++
		}
		c.Message = strings.Join(lines[start:i+1], "")

		if currentFile == "" {
			fmt.Fprintf(&errbuf, "unexpected comment before first file:\n\t%s\n", wrap(c.Message, "\t"))
			continue
		}
		c.Path = currentFile

		switch {
		case top:
			// per-file comment
		case side < 0:
			// comment on old file
			if old.Base == "" {
				c.Side = "PARENT"
			} else {
				c.PatchSet = old.BaseRev.PatchSetNumber
			}
			c.Line = lineOld - 1
		case side >= 0:
			// comment on new file or common text
			c.PatchSet = old.PatchRev.PatchSetNumber
			c.Line = lineNew - 1
		}

		if inReplyTo != nil {
			c.InReplyTo = inReplyTo.ID
		}

		for _, c0 := range drafts {
			if c0.Path == c.Path && c0.Side == c.Side && c0.Line == c.Line && c0.PatchSet == c.PatchSet && c0.InReplyTo == c.InReplyTo {
				c.ID = c0.ID
				delete(drafts, c0.ID)
			}
		}

		if *flagN {
			fmt.Fprintf(&errbuf, "add draft: %s\n", js(c))
		} else {
			revID := old.patchSetRevID(c.PatchSet)
			c.PatchSet = 0
			_, err := client.CreateDraft(old.ChangeInfo.ID, revID, &c)
			if err != nil {
				fmt.Fprintf(&errbuf, "saving draft: %v\n\t%s\n", err, wrap(c.Message, "\t"))
			}
		}
	}

	for _, c := range old.Drafts {
		if drafts[c.ID] != c {
			continue
		}
		if *flagN {
			fmt.Fprintf(&errbuf, "delete draft: %s\n", js(c))
		} else {
			revID := old.patchSetRevID(c.PatchSet)
			c.PatchSet = 0
			if err := client.DeleteDraft(old.ChangeInfo.ID, revID, c.ID); err != nil {
				fmt.Fprintf(&errbuf, "deleting draft: %v\n\t%s\n", err, wrap(c.Message, "\t"))
			}
		}
	}

	return nil
}

func (cl *CL) patchSetRevID(id int) string {
	for revID, rev := range cl.ChangeInfo.Revisions {
		if rev.PatchSetNumber == id {
			return revID
		}
	}
	return ""
}

func isCont(text string) bool {
	return strings.HasPrefix(text, "\t") || strings.TrimSpace(text) == ""
}

func isDraftLine(line string) bool {
	return !strings.HasPrefix(line, "File ") &&
		!strings.HasPrefix(line, DiffPrefix) &&
		!inlineCommentRE.MatchString(line)
}

func findComment(cl *CL, hdr, file string, side, lineOld, lineNew int) *gerrit.CommentInfo {
	for _, c := range cl.Comments[file] {
		line := lineNew - 1
		if side < 0 {
			line = lineOld - 1
		}
		if line < 0 {
			line = 0
		}
		if c.Line == line && commentHeader(c) == hdr {
			return c
		}
	}
	fmt.Fprintf(os.Stderr, "CANNOT FIND %q %q %q %d %d in %s\n", hdr, file, side, lineOld, lineNew, js(cl.Comments))
	return nil
}
