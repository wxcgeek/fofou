// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"strconv"
)

// url: /{forum}/postsBy?[user=${userNameInternal}][ip=${ipInternal}]
func handleList(w http.ResponseWriter, r *http.Request) {
	store := forum.Store
	query := parse8Bytes(r.FormValue("q"))
	isAdmin := forum.IsAdmin(getUser(r).ID)
	maxTopics := 50

	if count := r.FormValue("count"); isAdmin && count != "" {
		maxTopics, _ = strconv.Atoi(count)
	}

	posts, total := store.GetPostsBy(query, maxTopics)
	isBlocked := store.IsBlocked(query)

	model := struct {
		Forum
		Posts      []Post
		TotalCount int
		IsAdmin    bool
		Query      string
		Blocked    map[string]bool
		IsBlocked  bool
	}{
		Forum:      *forum,
		Posts:      posts,
		TotalCount: total,
		IsAdmin:    isAdmin,
		IsBlocked:  isBlocked,
		Query:      r.FormValue("q"),
	}

	if isAdmin {
		model.Blocked = make(map[string]bool)
		forum.Store.RLock()
		for k := range forum.Store.blocked {
			a, b := format8Bytes(k)
			model.Blocked[a] = true
			model.Blocked[b] = true
		}
		forum.Store.RUnlock()
	}

	ExecTemplate(w, tmplPosts, model)
}
