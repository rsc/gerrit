// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gerrit contains code to interact with Gerrit servers.
package gerrit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Client is a Gerrit client.
type Client struct {
	url  string // URL prefix, e.g. "https://go-review.googlesource.com/a" (without trailing slash)
	auth Auth

	// HTTPClient optionally specifies an HTTP client to use
	// instead of http.DefaultClient.
	HTTPClient *http.Client
}

// NewClient returns a new Gerrit client with the given URL prefix
// and authentication mode.
// The url should be just the scheme and hostname.
// If auth is nil, a default is used, or requests are made unauthenticated.
func NewClient(url string, auth Auth) *Client {
	if auth == nil {
		// TODO(bradfitz): use GitCookies auth, once that exists
		auth = NoAuth
	}
	return &Client{
		url:  strings.TrimSuffix(url, "/"),
		auth: auth,
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) do(dst interface{}, method, path string, arg url.Values, body interface{}) error {
	var bodyr io.Reader
	var contentType string
	if body != nil {
		v, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			return err
		}
		bodyr = bytes.NewReader(v)
		contentType = "application/json"
	}
	// slashA is either "/a" (for authenticated requests) or "" for unauthenticated.
	// See https://gerrit-review.googlesource.com/Documentation/rest-api.html#authentication
	slashA := "/a"
	if _, ok := c.auth.(noAuth); ok {
		slashA = ""
	}
	var err error
	u := c.url + slashA + path
	if arg != nil {
		u += "?" + arg.Encode()
	}
	req, err := http.NewRequest(method, u, bodyr)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.auth.setAuth(c, req)
	res, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode/10 != http.StatusOK/10 {
		body, _ := ioutil.ReadAll(io.LimitReader(res.Body, 4<<10))
		fmt.Fprintf(os.Stderr, "%s ==> %v\n", u, res.Status)
		return fmt.Errorf("HTTP status %s; %s", res.Status, body)
	}

	if dst == nil {
		return nil
	}

	// The JSON response begins with an XSRF-defeating header
	// like ")]}\n". Read that and skip it.
	br := bufio.NewReader(res.Body)
	if _, err := br.ReadSlice('\n'); err != nil {
		return err
	}
	data, err := ioutil.ReadAll(br)
	if err != nil {
		return err
	}
	/*
		if strings.HasSuffix(path, "/diff") {
			fmt.Printf("%s ==>\n%s\n", u, data)
		}
	*/

	err = json.Unmarshal(data, dst)
	if err != nil {
		fmt.Printf("%s ==> [%v]\n%s\n", u, err, data)
		return fmt.Errorf("%s: %v", u, err)
	}
	return nil
}

// ChangeInfo is a Gerrit data structure.
// See https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#change-info
type ChangeInfo struct {
	// ID is the ID of the change in the format
	// "'<project>~<branch>~<Change-Id>'", where 'project',
	// 'branch' and 'Change-Id' are URL encoded. For 'branch' the
	// refs/heads/ prefix is omitted.
	ID string `json:"id"`

	// The legacy numeric ID of the change.
	ChangeNumber int `json:"_number"`

	// The name of the project.
	Project string `json:"project"`

	// The name of the target branch.
	// The refs/heads/ prefix is omitted.
	Branch string `json:"branch"`

	// The topic to which this change belongs.
	Topic string `json:"topic"`

	// The Change-Id of the change.
	ChangeID string `json:"change_id"`

	// The subject of the change (header line of the commit message).
	Subject string `json:"subject"`

	// The status of the change (NEW, MERGED, ABANDONED, DRAFT).
	Status string `json:"status"`

	// When the change was created.
	Created TimeStamp `json:"created"`

	// When the change was last updated.
	Updated TimeStamp `json:"updated"`

	// Whether the calling user has starred this change.
	Starred bool `json:"starred"`

	// Whether the calling user has reviewed this change
	// (replied more recently than the owner).
	Reviewed bool `json:"reviewed"`

	// Whether the change can be merged.
	Mergeable bool `json:"mergeable"`

	// Number of inserted lines.
	Insertions int `json:"insertions"`

	// Number of deleted lines.
	Deletions int `json:"deletions"`

	// The owner of the change.
	Owner *AccountInfo `json:"owner"`

	// Actions the caller might be able to perform on this revision,
	// keyed by "view name" (TODO what is that?).
	Actions map[string]*ActionInfo `json:"actions"`

	// Labels applied to change.
	// Only set if LABELS or DETAILED_LABELS are requested.
	Labels map[string]LabelInfo `json:"labels"`

	// Values permitted for each label.
	// Only set if DETAILED_LABELS are requested.
	PermittedLabels map[string][]string `json:"permitted_labels"`

	// Reviewers that can be removed by the calling user.
	// Only set if DETAILED_LABELS are requested.
	RemovableReviewers []*AccountInfo `json:"removable_reviewers"`

	// Messages associated with the change.
	// Only set if MESSAGES are requested.
	Messages []*ChangeMessageInfo `json:"messages"`

	// Commit ID of the current patch set of this change.
	// Only set if CURRENT_REVISION or ALL_REVISIONS are requested.
	CurrentRevision string `json:"current_revision"`

	// Revisions indexed by patch set commit ID.
	// Only set if CURRENT_REVISION or ALL_REVISIONS are requested.
	Revisions map[string]*RevisionInfo `json:"revisions"`
}

// ActionInfo describes a REST API call the client can make to manipulate a resource.
// These are frequently implemented by plugins and may be discovered at runtime.
type ActionInfo struct {
	// HTTP method to use with the action.
	// Most actions use POST, PUT or DELETE to cause state changes.
	Method string `json;"method'`

	// Short title to display to a user describing the action.
	// In the Gerrit web interface the label is used as the text
	// on the button presented in the UI.
	Label string `json:"label"`

	// Longer text to display describing the action.
	// In a web UI this should be the title attribute of the element,
	// displaying when the user hovers the mouse.
	Title string `json:"title"`

	// If true the action is permitted at this time
	// and the caller is likely allowed to execute it.
	// This may change if state is updated at the server
	// or permissions are modified.
	Enabled bool `json:"enabled"`
}

type AccountInfo struct {
	// The numeric ID of the account.
	NumericID int64 `json:"_account_id"`

	// The full name of the user.
	// Only set if detailed account information is requested.
	Name string `json:"name,omitempty"`

	// The email address the user prefers to be contacted through.
	// Only set if detailed account information is requested.
	Email string `json:"email,omitempty"`

	// The username of the user.
	// Only set if detailed account information is requested.
	Username string `json:"username,omitempty"`

	// The approvals of the reviewer as a map that maps the label names
	// to the approval values (“-2”, “-1”, “0”, “+1”, “+2”).
	// For use when AccountInfo is being used as ReviewerInfo.
	Approvals map[string]string `json:"approvals,omitempty"`
}

func (ai *AccountInfo) Equal(v *AccountInfo) bool {
	if ai == nil || v == nil {
		return false
	}
	return ai.NumericID == v.NumericID
}

type ChangeMessageInfo struct {
	ID             string       `json:"id"`
	Author         *AccountInfo `json:"author"`
	Time           TimeStamp    `json:"date"`
	Message        string       `json:"message"`
	RevisionNumber int          `json:"_revision_number"`
}

// The LabelInfo entity contains information about a label on a
// change, always corresponding to the current patch set.
//
// There are two options that control the contents of LabelInfo:
// LABELS and DETAILED_LABELS.
//
// For a quick summary of the state of labels, use LABELS.
//
// For detailed information about labels, including exact numeric
// votes for all users and the allowed range of votes for the current
// user, use DETAILED_LABELS.
type LabelInfo struct {
	// Optional means the label may be set, but it's neither
	// necessary for submission nor does it block submission.
	Optional bool `json:"optional"`

	// Fields set by LABELS

	// One user who approved this label on the change
	// (voted the maximum value) as an AccountInfo entity.
	Approved *AccountInfo `json:"approved"`

	// One user who rejected this label on the change
	// (voted the minimum value) as an AccountInfo entity.
	Rejected *AccountInfo `json:"rejected"`

	// One user who recommended this label on the change
	// (voted positively, but not the maximum value) as an AccountInfo entity.
	Recommended *AccountInfo `json:"recommended"`

	// One user who disliked this label on the change
	// (voted negatively, but not the minimum value) as an AccountInfo entity.
	Disliked *AccountInfo `json:"disliked"`

	// Blocking means the label blocks the submit operation.
	Blocking bool `json:"blocking"`

	// The voting value of the user who recommended/disliked
	// this label on the change if it is not “+1”/“-1”.
	Value int `json:"value"`

	// The default voting value for the label. This value may be
	// outside the range specified in permitted_labels.
	DefaultValue int `json:"default_value"`

	// Fields set by DETAILED_LABELS

	// List of all approvals for this label.
	All []*ApprovalInfo `json:"all"`

	// All values that are allowed for this label.
	// The map maps the values (“-2”, “-1”, " `0`", “+1”, “+2”)
	// to the value descriptions.
	Values map[string]string `json:"values"`
}

type ApprovalInfo struct {
	AccountInfo
	Value int       `json:"value"`
	Date  TimeStamp `json:"date"`
}

// The RevisionInfo entity contains information about a patch set. Not
// all fields are returned by default. Additional fields can be
// obtained by adding o parameters as described at:
// https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#list-changes
type RevisionInfo struct {
	Draft          bool                  `json:"draft"`
	PatchSetNumber int                   `json:"_number"`
	Created        TimeStamp             `json:"created"`
	Uploader       *AccountInfo          `json:"uploader"`
	Ref            string                `json:"ref"`
	Fetch          map[string]*FetchInfo `json:"fetch"`
	Commit         *CommitInfo           `json:"commit"`
	Files          map[string]*FileInfo  `json:"files"`
	// TODO: more
}

type CommitInfo struct {
	Author    GitPersonInfo `json:"author"`
	Committer GitPersonInfo `json:"committer"`
	CommitID  string        `json:"commit"`
	Subject   string        `json:"subject"`
	Message   string        `json:"message"`
}

type GitPersonInfo struct {
	Name     string    `json:"name"`
	Email    string    `json:"Email"` // XXX really? disagrees with https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#git-person-info
	Date     TimeStamp `json:"date"`
	TZOffset int       `json:"tz"`
}

type FileInfo struct {
	Status        string `json:"status"`
	Binary        bool   `json:"binary"`
	OldPath       string `json:"old_path"`
	LinesInserted int    `json:"lines_inserted"`
	LinesDeleted  int    `json:"lines_deleted"`
}

type FetchInfo struct {
	URL      string            `json:"url"`
	Ref      string            `json:"ref"`
	Commands map[string]string `json:"commands"`
}

// QueryChangesOpt are options for QueryChanges.
type QueryChangesOpt struct {
	// N is the number of results to return.
	// If 0, the 'n' parameter is not sent to Gerrit.
	N int

	// Fields are optional fields to also return.
	// Example strings include "ALL_REVISIONS", "LABELS", "MESSAGES".
	// For a complete list, see:
	// https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#change-info
	Fields []string
}

func condInt(n int) []string {
	if n != 0 {
		return []string{strconv.Itoa(n)}
	}
	return nil
}

// QueryChanges queries changes. The q parameter is a Gerrit search query.
// For the API call, see https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#list-changes
// For the query syntax, see https://gerrit-review.googlesource.com/Documentation/user-search.html#_search_operators
func (c *Client) QueryChanges(q string, opts ...QueryChangesOpt) ([]*ChangeInfo, error) {
	var opt QueryChangesOpt
	switch len(opts) {
	case 0:
	case 1:
		opt = opts[0]
	default:
		return nil, errors.New("only 1 option struct supported")
	}
	var changes []*ChangeInfo
	err := c.do(&changes, "GET", "/changes/", url.Values{
		"q": {q},
		"n": condInt(opt.N),
		"o": opt.Fields,
	}, nil)
	return changes, err
}

// GetChangeDetail retrieves a change with labels, detailed labels, detailed
// accounts, and messages.
// For the API call, see https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#get-change-detail
func (c *Client) GetChangeDetail(changeID string, opts ...QueryChangesOpt) (*ChangeInfo, error) {
	var opt QueryChangesOpt
	switch len(opts) {
	case 0:
	case 1:
		opt = opts[0]
	default:
		return nil, errors.New("only 1 option struct supported")
	}
	var change ChangeInfo
	err := c.do(&change, "GET", "/changes/"+changeID+"/detail", url.Values{
		"o": opt.Fields,
	}, nil)
	if err != nil {
		return nil, err
	}
	return &change, nil
}

// A ReviewInput contains information for adding a review to a revision.
type ReviewInput struct {
	// Text to be added as review comment.
	Message string `json:"message,omitempty"`

	// Votes that should be added to the revision,
	// indexed by label name.
	Labels map[string]int `json:"labels,omitempty"`

	// Comments to be added,
	// indexed by file path.
	Comments map[string]*CommentInfo `json:"comments,omitempty"`

	// Whether all labels are required to be within the
	// user's permitted ranges based on access controls.
	// If true, attempting to use a label not granted to the user
	// will fail the entire modify operation early.
	// If false, the operation will execute anyway, but the proposed labels
	// will be modified to be the "best" value allowed by the access controls.
	StrictLabels bool `json:"strict_labels,omitempty"`

	// How to handle draft comments stored on server but not listed in the request.
	// Allowed values are DELETE, PUBLISH, PUBLISH_ALL_REVISIONS and KEEP.
	// All values except PUBLISH_ALL_REVISIONS operate only on drafts for a single revision.
	// The default is DELETE.
	Drafts string `json:"drafts,omitempty"`

	// Who to notify (email) after the review is stored.
	// Allowed values are NONE, OWNER, OWNER_REVIEWERS and ALL.
	// If not set, the default is ALL.
	Notify string `json:"notify,omitempty"`

	// The review should be posted on behalf of this account.
	// To use this option the caller must have been granted labelAs-NAME
	// permission for all keys of labels.
	OnBehalfOf string `json:"on_behalf_of,omitempty"`
}

type reviewInfo struct {
	Labels map[string]int `json:"labels,omitempty"`
}

// SetReview posts a review message on a change.
//
// For the API call, see https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#set-review
// The changeID is https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#change-id
// The revision is https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html#revision-id
func (c *Client) SetReview(changeID, revision string, review *ReviewInput) error {
	var res reviewInfo
	return c.do(&res, "POST", fmt.Sprintf("/changes/%s/revisions/%s/review", changeID, revision),
		nil, review)
}

// GetAccountInfo gets the specified account's information from Gerrit.
// For the API call, see https://gerrit-review.googlesource.com/Documentation/rest-api-accounts.html#get-account
// The accountID is https://gerrit-review.googlesource.com/Documentation/rest-api-accounts.html#account-id
//
// Note that getting "self" is a good way to validate host access, since it only requires peeker
// access to the host, not to any particular repository.
func (c *Client) GetAccountInfo(accountID string) (AccountInfo, error) {
	var res AccountInfo
	err := c.do(&res, "GET", fmt.Sprintf("/accounts/%s", accountID), nil, nil)
	return res, err
}

type TimeStamp time.Time

// Gerrit's timestamp layout is like time.RFC3339Nano, but with a space instead of the "T",
// and without a timezone (it's always in UTC).
const timeStampLayout = `"2006-01-02 15:04:05.999999999"`

func (ts *TimeStamp) MarshalJSON() ([]byte, error) {
	return []byte(ts.Time().Local().Format(timeStampLayout)), nil
}

func (ts *TimeStamp) UnmarshalJSON(p []byte) error {
	t, err := time.Parse(timeStampLayout, string(p))
	if err != nil {
		return errors.New("invalid time stamp format")
	}
	*ts = TimeStamp(t)
	return nil
}

func (ts TimeStamp) Time() time.Time { return time.Time(ts) }

// The DiffInfo entity contains information about the diff of a file in a revision.
//
// If the weblinks-only parameter is specified, only the web_links field is set.
type DiffInfo struct {
	// Meta information about the file on side A as a DiffFileMetaInfo entity.
	// Not present when the file is added.
	MetaA *DiffFileMetaInfo `json:"meta_a"`

	// Meta information about the file on side B as a DiffFileMetaInfo entity.
	// Not present when the file is deleted.
	MetaB *DiffFileMetaInfo `json:"meta_b"`

	// The type of change (ADDED, MODIFIED, DELETED, RENAMED COPIED, REWRITE).
	ChangeType string `json:"change_type"`

	// Intraline status (OK, ERROR, TIMEOUT).
	// Only set when the intraline parameter was specified in the request.
	IntralineStatus string `json:"intraline_status"`

	// A list of strings representing the patch set diff header.
	DiffHeader []string `json:"diff_header"`

	// The content differences in the file as a list of DiffContent entities.
	Content []*DiffContent `json:"content"`

	// Links to the file diff in external sites as a list of DiffWebLinkInfo entries.
	WebLinks []*DiffWebLinkInfo `json:"web_links"`

	// Whether the file is binary.
	Binary bool `json:"binary"`
}

// The DiffFileMetaInfo entity contains meta information about a file diff.
type DiffFileMetaInfo struct {
	Name        string             `json:"name"`         // name of file
	ContentType string             `json:"content_type"` // content type of file
	Lines       int                `json:"lines"`        // total number of lines in file
	WebLinks    []*DiffWebLinkInfo `json:"web_links"`    // links to file in external sites
}

// The DiffContent entity contains information about the content differences in a file.
type DiffContent struct {
	// Content only in A (deleted in B)
	A []string `json:"a"`

	// Content only in B (inserted in B)
	B []string `json:"b"`

	// Content on both sides (unchanged)
	AB []string `json:"ab"`

	// Text sections deleted from A.
	// Only present during a replace, i.e. both a and b are present.
	EditA DiffIntralineInfo `json:"edit_a"`

	// Text sections inserted in B.
	// Only present during a replace, i.e. both a and b are present.
	EditB DiffIntralineInfo `json:"edit_b"`

	// Count of lines skipped on both sides
	// when the file is too large to include all common lines.
	Skip int `json:"skip"`

	// Set to true if the region is common according to the requested
	// ignore-whitespace parameter, but a and b contain differing
	// amounts of whitespace. When present and true, a and b are
	// used instead of ab.
	Common bool `json:"common"`
}

// The DiffIntralineInfo entity contains information about intraline edits in a file.
//
// The information consists of a list of <skip length, mark length> pairs,
// where the skip length is the number of characters between
// the end of the previous edit and the start of this edit,
// and the mark length is the number of edited characters following the skip.
// The start of the edits is from the beginning of the related diff content lines.
//
// Note that the implied newline character at the end of each line
// is included in the length calculation, and thus it is possible for
// the edits to span newlines.
type DiffIntralineInfo [][]int

// The DiffWebLinkInfo entity describes a link on a diff screen to an external site.
type DiffWebLinkInfo struct {
	Name                     string `json:"name"`                           // link name
	URL                      string `json:"url"`                            // link URL
	ImageURL                 string `json:"image_url"`                      // URL for icon of link
	ShowOnSideBySideDiffView bool   `json:"show_on_side_by_side_diff_view"` // whether the web link should be shown on the side-by-side diff screen.
	ShowOnUnifiedDiffView    bool   `json:"show_on_unified_diff_view"`      // whether the web link should be shown on the unified diff screen.
}

// DiffOpt is options for QueryChanges.
type GetDiffOpt struct {
	// If the intraline parameter is specified, intraline differences are included in the diff.
	Intraline bool

	// The base parameter can be specified to control the base patch set from which the diff should be generated.
	Base string

	// If the weblinks-only parameter is specified, only the diff web links are returned.
	WebLinksOnly bool

	// The ignore-whitespace parameter can be specified to control how whitespace differences are reported in the result. Valid values are NONE, TRAILING, CHANGED or ALL.
	IgnoreWhitespace string

	// The context parameter can be specified to control the number of lines of surrounding context in the diff. Valid values are -1 (ALL) or number of lines.
	// Omitted if 0.
	Context int
}

// GetDiff gets the diff of a file from a certain revision.
func (c *Client) GetDiff(changeID, revID, filePath string, opts ...GetDiffOpt) (*DiffInfo, error) {
	var opt GetDiffOpt
	switch len(opts) {
	case 0:
	case 1:
		opt = opts[0]
	default:
		return nil, errors.New("only 1 option struct supported")
	}
	var diff DiffInfo
	v := url.Values{}
	if opt.Intraline {
		v["intraline"] = []string{""}
	}
	if opt.Base != "" {
		v["base"] = []string{opt.Base}
	}
	if opt.WebLinksOnly {
		v["weblinks-only"] = []string{""}
	}
	if opt.IgnoreWhitespace != "" {
		v["ignore-whitespace"] = []string{opt.IgnoreWhitespace}
	}
	if opt.Context == -1 {
		v["context"] = []string{"ALL"}
	}
	if opt.Context > 0 {
		v["context"] = []string{fmt.Sprint(opt.Context)}
	}

	err := c.do(&diff, "GET", "/changes/"+url.QueryEscape(changeID)+"/revisions/"+url.QueryEscape(revID)+"/files/"+url.QueryEscape(filePath)+"/diff", v, nil)
	if err != nil {
		return nil, err
	}
	return &diff, nil
}

// The CommentInfo entity contains information about an inline comment.
// This struct is also used in place of a Gerrit CommentInput.
type CommentInfo struct {
	// The patch set number for the comment; only set in contexts where
	// comments may be returned for multiple patch sets.
	PatchSet int `json:"patch_set,omitempty"`

	// The URL encoded UUID of the comment.
	ID string `json:"id,omitempty"`

	// The path of the file for which the inline comment was done.
	// Not set if returned in a map where the key is the file path.
	Path string `json:"path,omitempty"`

	// The side on which the comment was added.
	// Allowed values are REVISION and PARENT.
	// If not set, the default is REVISION.
	Side string `json:"side,omitempty"`

	// The number of the line for which the comment was done.
	// If range is set, this equals the end line of the range.
	// If neither line nor range is set, it's a file comment.
	Line int `json:"line,omitempty"`

	// The range of the comment as a CommentRange entity.
	Range *CommentRange `json:"range,omitempty"`

	// The URL encoded UUID of the comment to which this comment is a reply.
	InReplyTo string `json:"in_reply_to,omitempty"`

	// The comment message.
	Message string `json:"message,omitempty"`

	// The timestamp of when this comment was written.
	Updated *TimeStamp `json:"updated,omitempty"`

	// The author of the message as an AccountInfo entity.
	// Unset for draft comments, assumed to be the calling user.
	Author *AccountInfo `json:"author,omitempty"`
}

// IsDraft reports whether the comment is a draft.
func (c *CommentInfo) IsDraft() bool {
	return c.Author == nil
}

// AuthorName returns the name of the comment author.
// If the comment is a draft, AuthorName returns the empty string.
func (c *CommentInfo) AuthorName() string {
	if c.Author == nil {
		return ""
	}
	return c.Author.Name
}

// AuthorEmail returns the email address of the comment author.
// If the comment is a draft, AuthorEmail returns the empty string.
func (c *CommentInfo) AuthorEmail() string {
	if c.Author == nil {
		return ""
	}
	return c.Author.Email
}

// The CommentRange entity describes the range of an inline comment.
type CommentRange struct {
	StartLine int `json:"start_line"` // start line number
	StartChar int `json:"start_char"` // character position in start line
	EndLine   int `json:"end_line"`   // end line number
	EndChar   int `json:"end_char"`   // character position in end line
}

func (c *Client) listComments(url string) (map[string][]*CommentInfo, error) {
	m := make(map[string][]*CommentInfo)
	err := c.do(&m, "GET", url, nil, nil)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListChangeComments lists the published comments of all revisions of the change.
func (c *Client) ListChangeComments(changeID string) (map[string][]*CommentInfo, error) {
	return c.listComments("/changes/" + url.QueryEscape(changeID) + "/comments")
}

// ListChangeDrafts lists the current user's draft comments
// for all revisions of the change.
func (c *Client) ListChangeDrafts(changeID string) (map[string][]*CommentInfo, error) {
	return c.listComments("/changes/" + url.QueryEscape(changeID) + "/drafts")
}

// ListRevisionComments lists the published comments for the given revision.
// It returns a map keyed by file name.
func (c *Client) ListRevisionComments(changeID, revID string) (map[string][]*CommentInfo, error) {
	return c.listComments("/changes/" + url.QueryEscape(changeID) + "/revisions/" + url.QueryEscape(revID) + "/comments")
}

// ListRevisionDrafts lists the current user's draft comments for the given revision.
// It returns a map keyed by file name.
func (c *Client) ListRevisionDrafts(changeID, revID string) (map[string][]*CommentInfo, error) {
	return c.listComments("/changes/" + url.QueryEscape(changeID) + "/revisions/" + url.QueryEscape(revID) + "/drafts")
}

// TODO(rsc): Do we really need both CreateDraft and PutDraft?
// What if you call CreateDraft with draft.ID set?

// CreateDraft creates a draft comment on a revision.
func (c *Client) CreateDraft(changeID, revID string, draft *CommentInfo) (*CommentInfo, error) {
	var out CommentInfo
	err := c.do(&out, "PUT", "/changes/"+url.QueryEscape(changeID)+"/revisions/"+url.QueryEscape(revID)+"/drafts", nil, draft)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDraft retrieves a draft comment on a revision.
func (c *Client) GetDraft(changeID, revID, draftID string) (*CommentInfo, error) {
	var out CommentInfo
	err := c.do(&out, "GET", "/changes/"+url.QueryEscape(changeID)+"/revisions/"+url.QueryEscape(revID)+"/drafts/"+url.QueryEscape(draftID), nil, nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDraft updates a draft comment on a revision.
func (c *Client) UpdateDraft(changeID, revID, draftID string, draft *CommentInfo) (*CommentInfo, error) {
	var out CommentInfo
	err := c.do(&out, "PUT", "/changes/"+url.QueryEscape(changeID)+"/revisions/"+url.QueryEscape(revID)+"/drafts/"+url.QueryEscape(draftID), nil, nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDraft deletes a draft comment from a revision.
func (c *Client) DeleteDraft(changeID, revID, draftID string) error {
	return c.do(nil, "DELETE", "/changes/"+url.QueryEscape(changeID)+"/revisions/"+url.QueryEscape(revID)+"/drafts/"+url.QueryEscape(draftID), nil, nil)
}

// ListReviewers lists the reviewers of a change.
func (c *Client) ListReviewers(changeID string) ([]*AccountInfo, error) {
	var list []*AccountInfo
	err := c.do(&list, "GET", "/changes/"+url.QueryEscape(changeID)+"/reviewers", nil, nil)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// DeleteReviewer deletes a reviewer from a change.
func (c *Client) DeleteReviewer(changeID, accountID string) error {
	return c.do(nil, "DELETE", "/changes/"+url.QueryEscape(changeID)+"/reviewers/"+url.QueryEscape(accountID), nil, nil)
}

// AddReviewer adds one user or all members of a group to the change.
func (c *Client) AddReviewer(changeID string, rev *ReviewerInput) (*AddReviewerResult, error) {
	var out AddReviewerResult
	err := c.do(&out, "POST", "/changes/"+url.QueryEscape(changeID)+"/reviewers", nil, rev)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ReviewerInput contains information for adding a reviewer to a change.
type ReviewerInput struct {
	// The ID of one account that should be added as reviewer
	// or the ID of one group for which all members should be added as reviewers.
	Reviewer string `json:"reviewer,omitempty"`

	// Whether adding the reviewer is confirmed.
	// The Gerrit server may be configured to require a confirmation
	// when adding a group as reviewer that has many members.
	Confirmed bool `json:"confirmed,omitempty"`
}

// AddReviewerResult describes the result of adding a reviewer to a change.
type AddReviewerResult struct {
	// The newly added reviewers.
	Reviewers []*AccountInfo `json:"reviewers"`

	// Error message explaining why the reviewer could not be added.
	Error string `json:"error"`

	// Whether adding the reviewer requires confirmation.
	Confirm bool `json:"confirm"`
}

// SuggestReviewers lists the reviewers of a change.
func (c *Client) SuggestReviewers(changeID, query string, n int) ([]*SuggestedReviewerInfo, error) {
	var list []*SuggestedReviewerInfo
	err := c.do(&list, "GET", "/changes/"+url.QueryEscape(changeID)+"/suggest_reviewers",
		url.Values{"q": []string{query}, "n": []string{fmt.Sprint(n)}},
		nil)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// SuggestedReviewerInfo contains information about a reviewer that
// can be added to a change (an account or a group).
// SuggestedReviewerInfo has either Account or Group set.
type SuggestedReviewerInfo struct {
	Account *AccountInfo   `json:"account"`
	Group   *GroupBaseInfo `json:"group"`
}

// GroupBaseInfo contains base information about the group.
type GroupBaseInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Submit submits the change.
// It blocks until the change has been merged into the repository.
func (c *Client) Submit(changeID string) error {
	req := struct {
		Wait bool `json:"wait_for_merge"`
	}{
		true,
	}

	var ch ChangeInfo
	return c.do(&ch, "POST", "/changes/"+url.QueryEscape(changeID)+"/submit", nil, &req)
}

// Abandon abandons the change.
// It does not allow posting a message at the same time (but it could).
func (c *Client) Abandon(changeID string) error {
	var ch ChangeInfo
	return c.do(&ch, "POST", "/changes/"+url.QueryEscape(changeID)+"/abandon", nil, nil)
}
