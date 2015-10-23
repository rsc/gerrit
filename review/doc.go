// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Review is a client for reading and updating code reviews on a Gerrit server.

	usage: review [-a] [-e] [-h server] <query>

Review runs the query against the Gerrit server and prints a table of
matching code reviews, sorted by code review summary.
The default server is go-review.googlesource.com.

If multiple arguments are given as the query, review joins them by spaces
to form a single code review search. These two commands are equivalent:

	issue file:runtime reviewer:rsc
	issue "file:runtime reviewer:rsc"

Searches are always limited to pending reviews.

If the query is a single number N, review prints detailed information
about the code review with that numeric ID.

If the query is of the form N/P, review prints detailed information
about code review N's patch set P.

If the query is of the form N/B/P, review prints detailed information
about code review N's patch set P using patch set B as the base.

Authentication

Review looks in the files $HOME/.netrc and $HOME/.gitcookies for
authentication information for connecting to the Gerrit server.
These are the files where Gerrit suggests storing authentication data
for command-line use of the git command.
Gerrit used to use $HOME/.netrc but now uses $HOME/.gitcookies.
If you have neither, follow Gerrit's instructions to populate $HOME/.gitcookies.

Acme Editor Integration

If the -a flag is specified, review runs as a collection of acme windows
instead of a command-line tool. In this mode, the query is optional.
If no query is given, review uses "is:open".

There are four kinds of acme windows: review list, review, and patch set.

The following paths can be loaded (right clicked on) and open
a window (or navigate to an existing one).

	nnnn        review nnnn
	nnnn/p      review nnnn, patch set p
	nnnn/b/p    review nnnn, base patch set b, patch set p
	all         all pending code reviews

Executing "Search <query>" opens a new window showing the results
of that search.

Review List Window

A review list window displays a list of pending code reviews.
For example:

	XXX

Like in any window, right clicking on a review number opens a window
for that review.

Executing "Search <query>" opens a review list window showing only
the reviews matching the search. It shows the query in a header line.
For example:

	Search XXX

	XXX

Executing "Sort" in a review list window toggles between sorting by
title and sorting by decreasing code review number.

Review Window

A review window, opened by loading a review number, displays an overview
of a code review. The window starts with a header, then lists review scores,
and then shows the most recent patch set.

	Owner: bradfitz
	Reviewers: bradfitz, gobot, adg, bcmills, cespare
	Project: go
	Branch: master
	Updated: 85 minutes ago

	Code-Review:
	Run-TryBot: +1 bradfitz
	TryBot-Result: +1 gobot

	Patch Set 13
	 	net/http/httptest: change Server to use http.Server.ConnState for accounting

		With this CL, httptest.Server now uses connection-level accounting of
		outstanding requests instead of ServeHTTP-level accounting. This is
		more robust and results in a non-racy shutdown.

		This is much easier now that net/http.Server has the ConnState hook.

		Fixes #12789
		Fixes #12781

		Change-Id: I098cf334a6494316acb66cd07df90766df41764b

		Files:
		0		13/commit_message
		188		13/src/net/http/httptest/server.go
		27		13/src/net/http/httptest/server_test.go

	Comment by bradfitz (2015-10-19 11:08:02)

		Ping. This finally got into a good state last Friday.

	Comment by gobot (XXX)

		+1 TryBot-Result
		TryBots are happy.

	Comment by gobot (Oct 16 19:06)

		TryBots beginning.
		Status page: http://farmer.golang.org/try?commit=a1cd2b7d

	Comment by bradfitz (Oct 16 19:06)

		+1 Run-TryBot

	Patch Set 13 by bradfitz (Oct 16 19:01) 13 12/13
		0		13/commit_message
		188		13/src/net/http/httptest/server.go
		27		13/src/net/http/httptest/server_test.go

	Comment by bradfitz (Oct 16 18:19)

		⇢ 12/src/net/http/httptest/server.go:151
		These are *server* connections. These are the real ones we can do
		something about.

	The ones below are *client* connections, and may not even be the
	correct HTTP Transport if they made their own.
	(about half of overall HTTP tests do make their own Transport)

	⇢ 12/src/net/http/transport.go:54
	This is still required because we don't shut down StateNew things.
	This part of the CL prevents the socket late binding connections
	from ending up in StateNew and never leaving.

	« bcmills on Oct 16 18:13 » [12]
	⇢ 7/src/net/http/httptest/server.go:197
	If the solution is "don't do that", then we could make this a whole
	lot simpler by skipping the "blocks until all outstanding requests
	on this server have completed" part entirely.

	But that's a change it (at least the documented) semantics,
	which goes against the go1 compatibility policy.

	If we want to stick to the documented semantics, we need to fix that
	race - not just tell people not to expose it.  Otherwise it's fairly
	meaningless to talk about "outstanding requests on this server":
	anyone who relies on that behavior will still get spurious failures.

	I guess the alternative is to declare that blocking for outstanding
	requests is a bug in the documentation because it never worked
	correctly, but I'm not particularly comfortable with that.

	⇣ 12/src/net/http/httptest/server.go:151
	I'm still not entirely sure why we need this loop.
	Isn't the subsequent call to CloseIdleConnections sufficient
	to shut these down?

	It seems much simpler to only do wg.Done during the StateClosed/StateHijacked
	transition and to never Close the connections explicitly.
	(Instead of closing in StateIdle and StateNew, we'd only hit
	the CloseIdleConnections hammer again and let the client actually
	tear down the connection.)

	∙ 12/src/net/http/transport.go:54
	Do we still need all this closeGen stuff now that we're being more
	aggressive about calling CloseIdleConnections?

	(Or is this fundamentally racy because of the skew between the server
	and the client noticing that the connection is idle?)

	« bradfitz on Oct 16 18:08 » [12]
	Damn Mac builder time skew/clock resolution issue again.

Patch Set Window

	Owner: bradfitz
	Reviewers: bradfitz, gobot, adg, bcmills, cespare
	Project: go
	Branch: master
	Updated: 85 minutes ago

	Code-Review:
	Run-TryBot: +1 bradfitz
	TryBot-Result: +1 gobot

	File commit_message

		+Parent:     368f73bc (net: unblock plan9 TCP Read calls after socket close)
		+Author:     Brad Fitzpatrick <bradfitz@golang.org>
		+AuthorDate: 2015-09-29 14:26:48 -0700
		+Commit:     Brad Fitzpatrick <bradfitz@golang.org>
		+CommitDate: 2015-10-16 23:01:10 +0000
		+
		+net/http/httptest: change Server to use http.Server.ConnState for accounting
		+
		+With this CL, httptest.Server now uses connection-level accounting of
		+outstanding requests instead of ServeHTTP-level accounting. This is
		+more robust and results in a non-racy shutdown.
		+
		+This is much easier now that net/http.Server has the ConnState hook.
		+
		+Fixes #12789
		+Fixes #12781
		+
		+Change-Id: I098cf334a6494316acb66cd07df90766df41764b

	File src/net/http/httptest/server.go

	  @@ -1,64 +1,54 @@
	   // Copyright 2011 The Go Authors. All rights reserved.
	   // Use of this source code is governed by a BSD-style
	   // license that can be found in the LICENSE file.

	   // Implementation of Server

	   package httptest

	   import (
	  +       "bytes"
	          "crypto/tls"
	          "flag"
	          "fmt"
	  +       "log"
	          "net"
	          "net/http"
	          "os"
	  +       "runtime"
	          "sync"
	  +       "time"
	   )

	   // A Server is an HTTP server listening on a system-chosen port on the
	   // local loopback interface, for use in end-to-end HTTP tests.
	   type Server struct {
	          URL      string // base URL of form http://ipaddr:port with no trailing slash
	          Listener net.Listener

	          // TLS is the optional TLS configuration, populated with a new config
	          // after TLS is started. If set on an unstarted server before StartTLS
	          // is called, existing fields are copied into the new config.
	          TLS *tls.Config

	          // Config may be changed after calling NewUnstartedServer and
	          // before Start or StartTLS.
	          Config *http.Server

	          // wg counts the number of outstanding HTTP requests on this server.
	          // Close blocks until all requests are finished.
	          wg sync.WaitGroup
	  -}
	  -
	  -// historyListener keeps track of all connections that it's ever
	  -// accepted.
	  -type historyListener struct {
	  -       net.Listener
	  -       sync.Mutex // protects history
	  -       history    []net.Conn
	  -}
	  -
	  -func (hs *historyListener) Accept() (c net.Conn, err error) {
	  -       c, err = hs.Listener.Accept()
	  -       if err == nil {
	  -              hs.Lock()
	  -              hs.history = append(hs.history, c)
	  -              hs.Unlock()
	  -       }
	  -       return
	  +
	  +       mu     sync.Mutex // guards conns
	  +       closed bool
	  +       conns  map[net.Conn]http.ConnState // except terminal states
	   }

	   func newLocalListener() net.Listener {
	          if *serve != "" {
	                 l, err := net.Listen("tcp", *serve)
	                 if err != nil {
	                        panic(fmt.Sprintf("httptest: failed to listen on %v: %v", *serve, err))
	                 }
	                 return l
	          }
	  @@ -96,24 +86,23 @@
	                 Listener: newLocalListener(),
	                 Config:   &http.Server{Handler: handler},
	          }
	   }

	   // Start starts a server from NewUnstartedServer.
	   func (s *Server) Start() {
	          if s.URL != "" {
	                 panic("Server already started")
	          }
	  -       s.Listener = &historyListener{Listener: s.Listener}
	          s.URL = "http://" + s.Listener.Addr().String()
	  -       s.wrapHandler()
	  -       go s.Config.Serve(s.Listener)
	  +       s.wrap()
	  +       s.goServe()
	          if *serve != "" {
	                 fmt.Fprintln(os.Stderr, "httptest: serving on", s.URL)
	                 select {}
	          }
	   }

	   // StartTLS starts TLS on a server from NewUnstartedServer.
	   func (s *Server) StartTLS() {
	          if s.URL != "" {
	                 panic("Server already started")
	  @@ -127,84 +116,165 @@
	          s.TLS = new(tls.Config)
	          if existingConfig != nil {
	                 *s.TLS = *existingConfig
	          }
	          if s.TLS.NextProtos == nil {
	                 s.TLS.NextProtos = []string{"http/1.1"}
	          }
	          if len(s.TLS.Certificates) == 0 {
	                 s.TLS.Certificates = []tls.Certificate{cert}
	          }
	  -       tlsListener := tls.NewListener(s.Listener, s.TLS)
	  -
	  -       s.Listener = &historyListener{Listener: tlsListener}
	  +       s.Listener = tls.NewListener(s.Listener, s.TLS)
	          s.URL = "https://" + s.Listener.Addr().String()
	  -       s.wrapHandler()
	  -       go s.Config.Serve(s.Listener)
	  -}
	  -
	  -func (s *Server) wrapHandler() {
	  -       h := s.Config.Handler
	  -       if h == nil {
	  -              h = http.DefaultServeMux
	  -       }
	  -       s.Config.Handler = &waitGroupHandler{
	  -              s: s,
	  -              h: h,
	  -       }
	  +       s.wrap()
	  +       s.goServe()
	   }

	   // NewTLSServer starts and returns a new Server using TLS.
	   // The caller should call Close when finished, to shut it down.
	   func NewTLSServer(handler http.Handler) *Server {
	          ts := NewUnstartedServer(handler)
	          ts.StartTLS()
	          return ts
	   }

	  +type closeIdleTransport interface {
	  +       CloseIdleConnections()
	  +}
	  +
	   // Close shuts down the server and blocks until all outstanding
	   // requests on this server have completed.
	   func (s *Server) Close() {
	  -       s.Listener.Close()
	  -       s.wg.Wait()
	  -       s.CloseClientConnections()
	  -       if t, ok := http.DefaultTransport.(*http.Transport); ok {
	  +       s.mu.Lock()
	  +       if !s.closed {
	  +              s.closed = true
	  +              s.Listener.Close()
	  +              s.Config.SetKeepAlivesEnabled(false)
	  +              for c, st := range s.conns {

	Comment by bcmills on Oct 16 18:13

		I'm still not entirely sure why we need this loop.
		Isn't the subsequent call to CloseIdleConnections sufficient
		to shut these down?

		It seems much simpler to only do wg.Done during the StateClosed/StateHijacked
		transition and to never Close the connections explicitly.
		(Instead of closing in StateIdle and StateNew, we'd only hit
		the CloseIdleConnections hammer again and let the client actually
		tear down the connection.)

	Comment by bradfitz on Oct 16 18:19

		These are *server* connections. These are the real ones we can do
		something about.

		The ones below are *client* connections, and may not even be the
		correct HTTP Transport if they made their own.
		(about half of overall HTTP tests do make their own Transport)

	  +                     if st == http.StateIdle {
	  +                            s.closeConn(c)
	  +                     }
	  +              }
	  +              // If this server doesn't shut down in 5 seconds, tell the user why.
	  +              t := time.AfterFunc(5*time.Second, s.logCloseHangDebugInfo)
	  +              defer t.Stop()
	  +       }
	  +       s.mu.Unlock()
	  +
	  +       // Not part of httptest.Server's correctness, but assume most
	  +       // users of httptest.Server will be using the standard
	  +       // transport, so help them out and close any idle connections for them.
	  +       if t, ok := http.DefaultTransport.(closeIdleTransport); ok {
	                 t.CloseIdleConnections()
	          }
	  -}
	  -
	  -// CloseClientConnections closes any currently open HTTP connections
	  +
	  +       s.wg.Wait()
	  +}
	  +
	  +func (s *Server) logCloseHangDebugInfo() {
	  +       s.mu.Lock()
	  +       defer s.mu.Unlock()
	  +       var buf bytes.Buffer
	  +       buf.WriteString("httptest.Server blocked in Close after 5 seconds, waiting for connections:\
	  n")
	  +       for c, st := range s.conns {
	  +              fmt.Fprintf(&buf, "  %T %p %v in state %v\n", c, c, c.RemoteAddr(), st)
	  +       }
	  +       log.Print(buf.String())
	  +}
	  +
	  +// CloseClientConnections closes any currently-open HTTP connections
	   // to the test Server.
	   func (s *Server) CloseClientConnections() {
	  -       hl, ok := s.Listener.(*historyListener)
	  -       if !ok {
	  -              return
	  -       }
	  -       hl.Lock()
	  -       for _, conn := range hl.history {
	  -              conn.Close()
	  -       }
	  -       hl.Unlock()
	  -}
	  -
	  -// waitGroupHandler wraps a handler, incrementing and decrementing a
	  -// sync.WaitGroup on each request, to enable Server.Close to block
	  -// until outstanding requests are finished.
	  -type waitGroupHandler struct {
	  -       s *Server
	  -       h http.Handler // non-nil
	  -}
	  -
	  -func (h *waitGroupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	  -       h.s.wg.Add(1)
	  -       defer h.s.wg.Done() // a defer, in case ServeHTTP below panics
	  -       h.h.ServeHTTP(w, r)
	  +       s.mu.Lock()
	  +       defer s.mu.Unlock()
	  +       for c := range s.conns {
	  +              s.closeConn(c)
	  +       }
	  +}
	  +
	  +func (s *Server) goServe() {
	  +       s.wg.Add(1)
	  +       go func() {
	  +              defer s.wg.Done()
	  +              s.Config.Serve(s.Listener)
	  +       }()
	  +}
	  +
	  +// wrap installs the connection state-tracking hook to know which
	  +// connections are idle.
	  +func (s *Server) wrap() {
	  +       oldHook := s.Config.ConnState
	  +       s.Config.ConnState = func(c net.Conn, cs http.ConnState) {
	  +              s.mu.Lock()
	  +              defer s.mu.Unlock()
	  +              switch cs {
	  +              case http.StateNew:
	  +                     s.wg.Add(1)
	  +                     if s.conns == nil {
	  +                            s.conns = make(map[net.Conn]http.ConnState)
	  +                     }
	  +                     s.conns[c] = cs
	  +                     if s.closed {
	  +                            // Probably just a socket-late-binding dial from
	  +                            // the default transport that lost the race (and
	  +                            // thus this connection is now idle and will
	  +                            // never be used).
	  +                            s.closeConn(c)
	  +                     }
	  +              case http.StateActive:
	  +                     if oldState, ok := s.conns[c]; ok {
	  +                            if oldState != http.StateNew && oldState != http.StateIdle {
	  +                                   panic("invalid state transition")
	  +                            }
	  +                            s.conns[c] = cs
	  +                     }
	  +              case http.StateIdle:
	  +                     if oldState, ok := s.conns[c]; ok {
	  +                            if oldState != http.StateActive {
	  +                                   panic("invalid state transition")
	  +                            }
	  +                            s.conns[c] = cs
	  +                     }
	  +                     if s.closed {
	  +                            s.closeConn(c)
	  +                     }
	  +              case http.StateHijacked, http.StateClosed:
	  +                     s.forgetConn(c)
	  +              }
	  +              if oldHook != nil {
	  +                     oldHook(c, cs)
	  +              }
	  +       }
	  +}
	  +
	  +// closeConn closes c. Except on plan9, which is special. See comment below.
	  +// s.mu must be held.
	  +func (s *Server) closeConn(c net.Conn) {
	  +       if runtime.GOOS == "plan9" {
	  +              // Go's Plan 9 net package isn't great at unblocking reads when
	  +              // their underlying TCP connections are closed.  Don't trust
	  +              // that that the ConnState state machine will get to
	  +              // StateClosed. Instead, just go there directly. Plan 9 may leak
	  +              // resources if the syscall doesn't end up returning. Oh well.
	  +              s.forgetConn(c)
	  +       }
	  +       go c.Close()
	  +}
	  +
	  +// forgetConn removes c from the set of tracked conns and decrements it from the
	  +// waitgroup, unless it was previously removed.
	  +// s.mu must be held.
	  +func (s *Server) forgetConn(c net.Conn) {
	  +       if _, ok := s.conns[c]; ok {
	  +              delete(s.conns, c)
	  +              s.wg.Done()
	  +       }
	   }

	   // localhostCert is a PEM-encoded TLS cert with SAN IPs
	   // "127.0.0.1" and "[::1]", expiring at the last second of 2049 (the end
	   // of ASN.1 time).
	   // generated from src/crypto/tls:
	   // go run generate_cert.go  --rsa-bits 1024 --host 127.0.0.1,::1,example.com --ca --start-date "Jan
	  1 00:00:00 1970" --duration=1000000h
	   var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
	   MIICEzCCAXygAwIBAgIQMIMChMLGrR+QvmQvpwAU6zANBgkqhkiG9w0BAQsFADAS
	   MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw

 	net/http/httptest: change Server to use http.Server.ConnState for accounting

	With this CL, httptest.Server now uses connection-level accounting of
	outstanding requests instead of ServeHTTP-level accounting. This is
	more robust and results in a non-racy shutdown.

	This is much easier now that net/http.Server has the ConnState hook.

	Fixes #12789
	Fixes #12781

	Change-Id: I098cf334a6494316acb66cd07df90766df41764b

	Files:
	0		13/commit_message
	188		13/src/net/http/httptest/server.go
	27		13/src/net/http/httptest/server_test.go




Alternate Editor Integration

The -e flag enables basic editing of issues with editors other than acme.
The editor invoked is $VISUAL if set, $EDITOR if set, or else ed.
Review prepares a textual representation of code review data in a
temporary file, opens that file in the editor, waits for the editor
to exit, and then applies any changes from the file to the actual
code review.

Not yet implemented.

TODO: Describe what gets edited.
*/
package main
