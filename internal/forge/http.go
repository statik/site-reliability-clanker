package forge

import (
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

const (
	uploadPack  = "git-upload-pack"
	receivePack = "git-receive-pack"
)

// loader gives the git library the storage of the repository that a request names.
type loader struct{ forge *Forge }

func (l loader) Load(endpoint *transport.Endpoint) (storer.Storer, error) {
	s, err := l.forge.storage(strings.Trim(endpoint.Path, "/"))
	if err != nil {
		return nil, transport.ErrRepositoryNotFound
	}
	return s, nil
}

// HTTPHandler serves clone, fetch and push over the smart HTTP protocol of git, so that a
// git client can use a repository of the forge. Mount it at prefix, for example "/git/".
// The address of a repository is <prefix><name>.git.
func (f *Forge) HTTPHandler(prefix string) (http.Handler, error) {
	gitServer := server.NewServer(loader{forge: f})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, prefix)
		name, action, found := strings.Cut(rest, ".git/")
		if !found || name == "" || strings.Contains(name, "/") || !f.Exists(name) {
			http.NotFound(w, r)
			return
		}
		endpoint, err := transport.NewEndpoint("/" + name)
		if err != nil {
			http.Error(w, "the repository name is not valid", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		switch {
		case action == "info/refs" && r.Method == http.MethodGet:
			advertise(w, r, gitServer, endpoint)
		case action == uploadPack && r.Method == http.MethodPost:
			serveUploadPack(w, r, gitServer, endpoint)
		case action == receivePack && r.Method == http.MethodPost:
			serveReceivePack(w, r, gitServer, endpoint)
		default:
			http.NotFound(w, r)
		}
	}), nil
}

// fail answers a request that the git library could not serve. The client shows the text.
func fail(w http.ResponseWriter, what string, err error) {
	slog.Error("git over HTTP", "step", what, "error", err)
	http.Error(w, what+": "+err.Error(), http.StatusInternalServerError)
}

// advertise answers the first request of a client: the references of the repository.
func advertise(w http.ResponseWriter, r *http.Request, gitServer transport.Transport,
	endpoint *transport.Endpoint) {
	service := r.URL.Query().Get("service")
	var references *packp.AdvRefs
	switch service {
	case uploadPack:
		session, err := gitServer.NewUploadPackSession(endpoint, nil)
		if err != nil {
			fail(w, "open the repository", err)
			return
		}
		defer session.Close()
		if references, err = session.AdvertisedReferencesContext(r.Context()); err != nil {
			fail(w, "list the references", err)
			return
		}
	case receivePack:
		session, err := gitServer.NewReceivePackSession(endpoint, nil)
		if err != nil {
			fail(w, "open the repository", err)
			return
		}
		defer session.Close()
		if references, err = session.AdvertisedReferencesContext(r.Context()); err != nil {
			fail(w, "list the references", err)
			return
		}
	default:
		http.Error(w, "the forge serves only the smart HTTP protocol of git", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
	encoder := pktline.NewEncoder(w)
	if err := encoder.Encodef("# service=%s\n", service); err != nil {
		return
	}
	if err := encoder.Flush(); err != nil {
		return
	}
	if err := references.Encode(w); err != nil {
		slog.Error("git over HTTP", "step", "send the references", "error", err)
	}
}

// body returns the request body, unpacked when the client compressed it.
func body(r *http.Request) (io.ReadCloser, error) {
	if r.Header.Get("Content-Encoding") != "gzip" {
		return r.Body, nil
	}
	unpacked, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, fmt.Errorf("unpack the request: %w", err)
	}
	return unpacked, nil
}

func serveUploadPack(w http.ResponseWriter, r *http.Request, gitServer transport.Transport,
	endpoint *transport.Endpoint) {
	input, err := body(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer input.Close()
	request := packp.NewUploadPackRequest()
	if err := request.Decode(input); err != nil {
		http.Error(w, "read the fetch request: "+err.Error(), http.StatusBadRequest)
		return
	}
	session, err := gitServer.NewUploadPackSession(endpoint, nil)
	if err != nil {
		fail(w, "open the repository", err)
		return
	}
	defer session.Close()
	response, err := session.UploadPack(r.Context(), request)
	if err != nil {
		fail(w, "make the pack", err)
		return
	}
	defer response.Close()
	w.Header().Set("Content-Type", "application/x-"+uploadPack+"-result")
	if err := response.Encode(w); err != nil {
		slog.Error("git over HTTP", "step", "send the pack", "error", err)
	}
}

// changesMain reports whether a push creates, moves or removes the main branch. The main
// branch is what the control plane plans and applies, and it holds the policy rules. It
// changes only when the forge merges a pull request after the approvals.
func changesMain(request *packp.ReferenceUpdateRequest) bool {
	for _, command := range request.Commands {
		if command.Name == branchRef(mainBranch) {
			return true
		}
	}
	return false
}

func serveReceivePack(w http.ResponseWriter, r *http.Request, gitServer transport.Transport,
	endpoint *transport.Endpoint) {
	input, err := body(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer input.Close()
	request := packp.NewReferenceUpdateRequest()
	if err := request.Decode(input); err != nil {
		http.Error(w, "read the push request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if changesMain(request) {
		http.Error(w, "the branch main changes only through a pull request: push another "+
			"branch and open a pull request", http.StatusForbidden)
		return
	}
	session, err := gitServer.NewReceivePackSession(endpoint, nil)
	if err != nil {
		fail(w, "open the repository", err)
		return
	}
	defer session.Close()
	// The status says for each reference if the push worked. A failure of one reference
	// is in the status, and the error repeats it.
	status, err := session.ReceivePack(r.Context(), request)
	if status == nil {
		fail(w, "store the push", err)
		return
	}
	if err != nil {
		slog.Warn("git over HTTP", "step", "store the push", "error", err)
	}
	w.Header().Set("Content-Type", "application/x-"+receivePack+"-result")
	if err := status.Encode(w); err != nil {
		slog.Error("git over HTTP", "step", "send the push status", "error", err)
	}
}
