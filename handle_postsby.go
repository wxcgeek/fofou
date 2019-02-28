// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"strconv"

	"github.com/coyove/fofou/server"
)

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
		*Forum
		*Topic
		TotalCount int
		IsAdmin    bool
		Query      string
		Blocked    map[string]bool
		IsBlocked  bool
	}{
		Forum:      forum,
		Topic:      &Topic{Posts: posts},
		TotalCount: total,
		IsAdmin:    isAdmin,
		IsBlocked:  isBlocked,
		Query:      r.FormValue("q"),
	}

	server.Render(w, server.TmplPosts, model)
}
