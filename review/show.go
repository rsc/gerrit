package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"rsc.io/gerrit/internal/gerrit"
)

type CL struct {
	ChangeInfo *gerrit.ChangeInfo
	Reviewers  []*gerrit.AccountInfo
	Comments   map[string][]*gerrit.CommentInfo
	PatchID    string
	PatchRev   *gerrit.RevisionInfo
	Base       string
	BaseRev    *gerrit.RevisionInfo
	Drafts     []*gerrit.CommentInfo
}

func showQuery(w io.Writer, q string) error {
	all, err := searchIssues(q)
	if err != nil {
		return err
	}
	sort.Sort(clsBySubject(all))

	for _, ch := range all {
		suffix := " ["
		suffix += shortEmail(ch.Owner.Email)
		suffix += fmt.Sprintf(", +%d-%d", ch.Insertions, ch.Deletions)
		label, ok := ch.Labels["Code-Review"]
		if ok {
			for _, vote := range label.All {
				if vote.Value != 0 {
					suffix += fmt.Sprintf(", %s%+d", shortEmail(vote.Email), vote.Value)
				}
			}
		}
		suffix += "]"
		if ch.Starred {
			suffix += " \u2606"
		}
		if !ch.Reviewed {
			suffix += " NEW"
		}
		fmt.Fprintf(w, "%d\t%s\t%s%s\n", ch.ChangeNumber, ch.Project, ch.Subject, suffix)
	}
	return nil
}

func searchIssues(q string) ([]*gerrit.ChangeInfo, error) {
	chs, err := client.QueryChanges("is:open -project:scratch -message:do-not-review "+q, gerrit.QueryChangesOpt{
		Fields: []string{
			"DETAILED_ACCOUNTS",
		},
	})
	if err != nil {
		return nil, err
	}
	return chs, nil
}

type clsBySubject []*gerrit.ChangeInfo

func (x clsBySubject) Len() int      { return len(x) }
func (x clsBySubject) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x clsBySubject) Less(i, j int) bool {
	if x[i].Project != x[j].Project {
		return x[i].Project < x[j].Project
	}
	if x[i].Subject != x[j].Subject {
		return x[i].Subject < x[j].Subject
	}
	return x[i].ChangeNumber < x[j].ChangeNumber
}

func shortEmail(x string) string {
	i := strings.Index(x, "@")
	if i >= 0 {
		return x[:i]
	}
	return x
}

func shortTime(t gerrit.TimeStamp) string {
	return t.Time().Format(time.Stamp)
}

func wrap(t string, prefix string) string {
	const max = 80
	out := ""
	t = strings.Replace(t, "\r\n", "\n", -1)
	lines := strings.Split(t, "\n")
	for i, line := range lines {
		if i > 0 {
			out += "\n" + prefix
		}
		s := line
		for len(s) > max {
			i := strings.LastIndex(s[:max], " ")
			if i < 0 {
				i = max - 1
			}
			i++
			out += s[:i] + "\n" + prefix
			s = s[i:]
		}
		out += s
	}
	return out
}

func showCL(w io.Writer, id int) (*CL, error) {
	var cl CL
	ch, err := client.GetChangeDetail(fmt.Sprint(id), gerrit.QueryChangesOpt{
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
		return nil, err
	}
	cl.ChangeInfo = ch

	reviewers, err := client.ListReviewers(ch.ID)
	if err != nil {
		return nil, err
	}
	cl.Reviewers = reviewers

	fmt.Fprintf(w, "# Project: %s\n", ch.Project)
	fmt.Fprintf(w, "# Branch: %s\n", ch.Branch)
	fmt.Fprintf(w, "# Created: %s\n", shortTime(ch.Created))
	fmt.Fprintf(w, "# Updated: %s\n", shortTime(ch.Updated))
	fmt.Fprintf(w, "# URL: https://go-review.googlesource.com/%v\n", ch.ChangeNumber)
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "Owner: %s\n", shortEmail(ch.Owner.Email))
	fmt.Fprintf(w, "Reviewers:")
	for _, r := range reviewers {
		if !r.Equal(ch.Owner) {
			fmt.Fprintf(w, " %s", shortEmail(r.Email))
		}
	}
	fmt.Fprintf(w, "\n")
	for name, label := range ch.Labels {
		fmt.Fprintf(w, "%s: ", name)
		for _, vote := range label.All {
			if vote.Value != 0 {
				fmt.Fprintf(w, "%s%+d ", shortEmail(vote.Email), vote.Value)
			}
		}
		fmt.Fprintf(w, "\n")
	}
	fmt.Fprintf(w, "\n")

	rev := ch.Revisions[ch.CurrentRevision]
	fmt.Fprintf(w, "<optional comment here>\n\n")
	fmt.Fprintf(w, "Patch Set %d (%d.%d)\n\n", rev.PatchSetNumber, ch.ChangeNumber, rev.PatchSetNumber)
	c := rev.Commit
	fmt.Fprintf(w, "\t%s\n", wrap(c.Message, "\t"))
	fmt.Fprintf(w, "\tAuthor: %s <%s> %s\n", c.Author.Name, c.Author.Email, shortTime(c.Author.Date))
	fmt.Fprintf(w, "\tCommitter: %s <%s> %s\n\n", c.Committer.Name, c.Committer.Email, shortTime(c.Committer.Date))
	for name, file := range rev.Files {
		fmt.Fprintf(w, "\t%s +%d -%d\n", name, file.LinesInserted, file.LinesDeleted)
	}
	fmt.Fprintf(w, "\n")

	msgs, err := client.ListChangeComments(ch.ID)
	if err != nil {
		return nil, err
	}
	cl.Comments = msgs

	drafts, err := client.ListChangeDrafts(ch.ID)
	if err != nil {
		return nil, err
	}
	for file, list := range drafts {
		msgs[file] = append(msgs[file], list...)
	}

	var files []string
	for file := range msgs {
		files = append(files, file)
	}
	sort.Strings(files)

	for _, m := range ch.Messages {
		who := "Gerrit"
		if m.Author != nil {
			who = shortEmail(m.Author.Email)
		}
		fmt.Fprintf(w, "Comment by %s (%s)\n", who, shortTime(m.Time))
		fmt.Fprintf(w, "\n\t%s\n", wrap(m.Message, "\t"))
		fmt.Fprintf(w, "\n")
		for _, file := range files {
			kept := msgs[file][:0]
			for _, msg := range msgs[file] {
				if msg.Author != nil && msg.Author.Equal(m.Author) && msg.Updated.Time().Equal(m.Time.Time()) {
					fmt.Fprintf(w, "\t> %s:%d\n\n\t%s\n\n", file, msg.Line, wrap(msg.Message, "\t"))
				} else {
					kept = append(kept, msg)
				}
			}
			msgs[file] = kept
		}
	}

	/*
		for _, file := range files {
			for _, m := range msgs[file] {
				fmt.Fprintf(w, "Comment by %s (%s)\n\n", shortEmail(m.Author.Email), shortTime(*m.Updated))
				fmt.Fprintf(w, "\t> %s:%d\n\n\t%s\n\n", file, m.Line, wrap(m.Message, "\t"))
			}
		}
	*/
	return &cl, nil
}

const DiffPrefix = "\u22ee"

func showPatchSet(w io.Writer, id, base, patch int) (*CL, error) {
	var cl CL
	ch, err := client.GetChangeDetail(fmt.Sprint(id), gerrit.QueryChangesOpt{
		Fields: []string{
			"ALL_REVISIONS",
			"DETAILED_ACCOUNTS",
			"DETAILED_LABELS",
			"ALL_COMMITS",
			"ALL_FILES",
		},
	})
	if err != nil {
		return nil, err
	}
	cl.ChangeInfo = ch

	patchID := ""
	var patchRev *gerrit.RevisionInfo
	for revID, rev := range ch.Revisions {
		if rev.PatchSetNumber == patch {
			patchID = revID
			patchRev = rev
			break
		}
	}
	if patchRev == nil {
		return nil, fmt.Errorf("unknown patch set %d.%d", id, patch)
	}
	cl.PatchID = patchID
	cl.PatchRev = patchRev

	opt := gerrit.GetDiffOpt{
		// We use the full file context even to prepare shorter diff views.
		// The Gerrit server seems to send full context no matter what,
		// so this line is not strictly necessary, but in case that apparent
		// bug gets fixed, ask for full context explicitly.
		Context: -1,
	}
	if base != 0 {
		for revID, rev := range ch.Revisions {
			if rev.PatchSetNumber == base {
				opt.Base = revID
				cl.Base = opt.Base
				cl.BaseRev = rev
				goto FoundBase
			}
		}
		return nil, fmt.Errorf("unknown patch set base %d", base)
	FoundBase:
	}

	msgs, err := client.ListRevisionComments(ch.ID, patchID)
	if err != nil {
		return nil, err
	}
	cl.Comments = msgs
	drafts, err := client.ListRevisionDrafts(ch.ID, patchID)
	if err != nil {
		return nil, err
	}
	for file, list := range drafts {
		msgs[file] = append(msgs[file], list...)
	}

	if opt.Base != "" {
		for file, list := range msgs {
			out := list[:0]
			for _, m := range list {
				if m.Side != "PARENT" {
					out = append(out, m)
				}
			}
			msgs[file] = out
		}

		msgsBase, err := client.ListRevisionComments(ch.ID, opt.Base)
		if err != nil {
			return nil, err
		}
		drafts, err := client.ListRevisionDrafts(ch.ID, patchID)
		if err != nil {
			return nil, err
		}
		for file, list := range drafts {
			msgsBase[file] = append(msgsBase[file], list...)
		}

		for file, list := range msgsBase {
			for _, m := range list {
				m.Side = "PARENT"
			}
			msgs[file] = append(msgs[file], list...)
		}
	}

	baseStr := ""
	if base != 0 {
		baseStr = fmt.Sprintf(" (against base patch set %d)", base)
	}
	fmt.Fprintf(w, "CL %d Patch Set %d%s\n", id, patch, baseStr)
	fmt.Fprintf(w, "\n")

	var files []string
	for file := range patchRev.Files {
		files = append(files, file)
	}
	sort.Strings(files)

	for _, file := range files {
		const maxContext = 3
		fmt.Fprintf(w, "File %s\n\n", file)

		diff, err := client.GetDiff(ch.ID, patchID, file, opt)

		var oldMsgs, newMsgs []*gerrit.CommentInfo
		for _, m := range msgs[file] {
			if m.Side == "PARENT" {
				oldMsgs = append(oldMsgs, m)
			} else {
				newMsgs = append(newMsgs, m)
			}
		}
		sort.Sort(msgsByDisplay(oldMsgs))
		sort.Sort(msgsByDisplay(newMsgs))

		sep := ""
		if err != nil {
			fmt.Fprintf(w, "ERROR: %v\n", err)
		} else {
			udiff := formatUnifiedDiff(diff)
			printMsg := func(m *gerrit.CommentInfo, isNew bool) {
				if m.IsDraft() {
					fmt.Fprintf(w, "%s%s\n\n", sep, m.Message)
					m.Side = ""
					if isNew {
						m.PatchSet = patchRev.PatchSetNumber
					} else if base != 0 {
						m.PatchSet = base
					} else {
						m.PatchSet = 0
						m.Side = "PARENT"
					}
					cl.Drafts = append(cl.Drafts, m)
				} else {
					fmt.Fprintf(w, "%s%s\n\n", sep, commentHeader(m))
					fmt.Fprintf(w, "\t%s\n\n", wrap(m.Message, "\t"))
				}
				sep = ""
			}
			for len(oldMsgs) > 0 && oldMsgs[0].Line == 0 {
				printMsg(oldMsgs[0], false)
				oldMsgs = oldMsgs[1:]
			}
			for len(newMsgs) > 0 && newMsgs[0].Line == 0 {
				printMsg(newMsgs[0], true)
				newMsgs = newMsgs[1:]
			}
			for _, line := range udiff {
				fmt.Fprintf(w, "%s%s%s\n", DiffPrefix, line.Prefix, line.Text)
				sep = "\n"
				for len(oldMsgs) > 0 && oldMsgs[0].Line <= line.Old {
					printMsg(oldMsgs[0], false)
					oldMsgs = oldMsgs[1:]
				}
				for len(newMsgs) > 0 && newMsgs[0].Line <= line.New {
					printMsg(newMsgs[0], true)
					newMsgs = newMsgs[1:]
				}
			}
			for _, m := range oldMsgs {
				printMsg(m, false)
			}
			for _, m := range newMsgs {
				printMsg(m, true)
			}
		}
		fmt.Fprint(w, sep)
	}
	return &cl, nil
}

type msgsByDisplay []*gerrit.CommentInfo

func (x msgsByDisplay) Len() int      { return len(x) }
func (x msgsByDisplay) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x msgsByDisplay) Less(i, j int) bool {
	if x[i].Line != x[j].Line {
		return x[i].Line < x[j].Line
	}
	if x[i].IsDraft() != x[j].IsDraft() {
		return x[j].IsDraft()
	}
	return x[i].Updated.Time().Before(x[j].Updated.Time())
}

type Line struct {
	Prefix string
	Text   string
	Old    int
	New    int
}

func formatUnifiedDiff(diff *gerrit.DiffInfo) []Line {
	var out []Line
	for _, line := range diff.DiffHeader {
		out = append(out, Line{Text: line})
	}

	content := diff.Content
	oldLine := 1
	newLine := 1
	const maxContext = 3
	decl := ""
	for len(content) > 0 {
		// Leading common chunk always included.
		i := 0
		if len(content[i].AB) > 0 {
			i++
		}
		// Collect a run until too large a common chunk or EOF.
		for i < len(content) && len(content[i].AB) <= 2*maxContext {
			i++
		}
		run := content[:i]
		if i < len(content) && len(content[i].AB) > 2*maxContext {
			run = content[:i+1]
		}
		content = content[i:]

		// Do not emit hunk with nothing but common lines at end of file.
		if len(content) == 0 && len(run) == 1 && len(run[0].AB) > 0 {
			break
		}

		// Format that run into a diff chunk.
		oldStart := oldLine
		newStart := newLine
		startDecl := decl
		var chunk []Line
		for i, c := range run {
			if len(c.AB) > 0 {
				lines := c.AB
				if i == 0 && len(c.AB) > maxContext {
					skip := len(c.AB) - maxContext
					for _, line := range c.AB[:skip] {
						if isDecl(line) {
							decl = " " + line
							startDecl = decl
						}
					}
					oldStart += skip
					newStart += skip
					oldLine += skip
					newLine += skip
					lines = lines[skip:]
				} else if i == len(run)-1 && len(c.AB) > maxContext {
					lines = lines[:maxContext]
				}
				for _, line := range lines {
					chunk = append(chunk, Line{Prefix: " ", Text: line, Old: oldLine, New: newLine})
					oldLine++
					newLine++
					if isDecl(line) {
						decl = " " + line
					}
				}
			} else {
				for _, line := range c.A {
					chunk = append(chunk, Line{Prefix: "-", Text: line, Old: oldLine, New: 0})
					oldLine++
				}
				for _, line := range c.B {
					chunk = append(chunk, Line{Prefix: "+", Text: line, Old: 0, New: newLine})
					newLine++
					if isDecl(line) {
						decl = " " + line
					}
				}
			}
		}
		oldEnd := oldLine
		newEnd := newLine
		if c := run[len(run)-1]; len(c.AB) > maxContext {
			// Set up correctly for next loop over content,
			// which may reprocess this section.
			oldLine -= maxContext
			newLine -= maxContext
		}

		if len(startDecl) > 55 {
			startDecl = startDecl[:50] + "..."
		}
		out = append(out, Line{Text: fmt.Sprintf("@@ -%d,%d +%d,%d @@%s", oldStart, oldEnd-oldStart, newStart, newEnd-newStart, startDecl)})
		out = append(out, chunk...)
	}

	return out
}

func isDecl(x string) bool {
	return len(x) > 0 && x[0] != '\n' && x[0] != ' ' && x[0] != '\t' && x[0] != '\r'
}

func commentHeader(c *gerrit.CommentInfo) string {
	who := "draft xxx"
	if c.Author != nil {
		who = shortEmail(c.Author.Email)
	}
	return fmt.Sprintf("%s (%s):", who, shortTime(*c.Updated))
}
