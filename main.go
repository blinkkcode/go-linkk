package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/memcache"
	"google.golang.org/appengine/user"
)

func init() {
	http.HandleFunc("/_/api/edit", apiCreateHandler)
	http.HandleFunc("/~/", infoHandler)
	http.HandleFunc("/", redirectHandler)
}

func apiCreateHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	u := user.Current(ctx)
	err := authUserDomain(u)
	if err != nil {
		writeJSONError(ctx, w, err, "", http.StatusUnauthorized)
		return
	}

	if r.Method == "POST" {
		linkk := &Linkk{
			Path:    r.FormValue("path"),
			URL:     r.FormValue("url"),
			Comment: r.FormValue("comment"),
		}
		linkk.Clean()
		err = linkk.Validate()
		if err != nil {
			writeJSONError(ctx, w, err, "", http.StatusInternalServerError)
			return
		}

		// Test for existing object to overwrite.
		key, _, err := getLinkkByPath(ctx, linkk.Path)
		if err != nil {
			writeJSONError(ctx, w, err, "Unable to search for linkk", http.StatusInternalServerError)
			return
		}
		if key == nil {
			key = datastore.NewIncompleteKey(ctx, "Linkk", nil)
		}
		if _, err := datastore.Put(ctx, key, linkk); err != nil {
			writeJSONError(ctx, w, err, "Unable to store new linkk", http.StatusInternalServerError)
			return
		}

		writeJSONResponse(ctx, w, EntityResponse{Key: key, Entity: linkk})
	}
}

func authUserDomain(u *user.User) error {
	if !appengine.IsDevAppServer() && u.AuthDomain != "gmail.com" {
		return fmt.Errorf("Invalid auth domain set for authorization: %s", u.AuthDomain)
	}

	domains := getAuthDomains()

	if appengine.IsDevAppServer() {
		domains = append(domains, "example.com")
	}

	if len(domains) == 0 {
		return errors.New("No auth domains configured for authorization")
	}

	r, _ := regexp.Compile("@(.*)$")
	domain := r.FindStringSubmatch(u.Email)

	if !stringInSlice(domain[1], domains) {
		return fmt.Errorf("Invalid authorization domain: %s", domain[1])
	}

	return nil
}

func getAuthDomains() []string {
	return strings.Split(os.Getenv("AUTH_DOMAINS"), "|")
}

func getLinkkByPath(ctx context.Context, path string) (key *datastore.Key, linkk *Linkk, err error) {
	path = strings.ToLower(path)
	q := datastore.NewQuery("Linkk").Filter("Path =", path)
	t := q.Run(ctx)
	for {
		var linkk Linkk
		key, err := t.Next(&linkk)
		if err == datastore.Done {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		return key, &linkk, nil
	}
	// No linkk found.
	return nil, nil, nil
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	path := r.URL.Path[2:]

	// Search for the path in the existing linkks.
	key, linkk, err := getLinkkByPath(ctx, path)
	if err != nil {
		writeJSONError(ctx, w, err, "Unable to search for linkk", http.StatusInternalServerError)
		return
	}

	writeJSONResponse(ctx, w, EntityResponse{Key: key, Entity: linkk})
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	// Root path, redirect to edit ui.
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/_/ui/edit/index.html", 302)
	}

	// Get the linkk from cache if available.
	if item, err := memcache.Get(ctx, r.URL.Path); err == memcache.ErrCacheMiss {
		// Not found, ignore.
	} else if err != nil {
		writeJSONError(ctx, w, err, "Unable to check cache for linkk", http.StatusInternalServerError)
		return
	} else {
		http.Redirect(w, r, string(item.Value), 302)
		return
	}

	// Search for the path in the existing linkks.
	_, linkk, err := getLinkkByPath(ctx, r.URL.Path)
	if err != nil {
		writeJSONError(ctx, w, err, "Unable to search for linkk", http.StatusInternalServerError)
		return
	}

	// Save to cache and redirect.
	if linkk != nil {
		item := &memcache.Item{
			Key:   linkk.Path,
			Value: []byte(linkk.URL),
		}
		if err := memcache.Set(ctx, item); err != nil {
			return
		}
		http.Redirect(w, r, linkk.URL, 302)
		return
	}

	// Not found, redirect to page to edit the redirect.
	http.Redirect(w, r, fmt.Sprintf("/_/ui/edit/index.html?path=%s", url.QueryEscape(r.URL.Path)), 302)
}
