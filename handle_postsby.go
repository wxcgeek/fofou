// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

// url: /{forum}/postsBy?[user=${userNameInternal}][ip=${ipInternal}]
func handlePostsBy(w http.ResponseWriter, r *http.Request) {
	forum := mustGetForum(w, r)
	if forum == nil {
		return
	}
	store := forum.Store

	var posts []Post
	userInternal := strings.TrimSpace(r.FormValue("user"))
	ipAddrInternal := strings.TrimSpace(r.FormValue("ip"))
	if userInternal == "" && ipAddrInternal == "" {
		logger.Noticef("handlePostsBy(): missing both user and ip")
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}

	isAdmin := userIsAdmin(forum, getSecureCookie(r))
	maxTopics := 50
	if count := r.FormValue("count"); isAdmin && count != "" {
		maxTopics, _ = strconv.Atoi(count)
	}
	var total int
	if userInternal != "" {
		posts, total = store.GetPostsByUserInternal(userInternal, maxTopics)
	} else {
		posts, total = store.GetPostsByIPInternal(ipAddrInternal, maxTopics)
	}

	var ipAddr string
	ipBlocked, userBlocked := false, store.IsBlocked("u"+userInternal)
	if ipAddrInternal != "" {
		ipAddr = ipAddrInternalToOriginal(ipAddrInternal)
		ipBlocked = store.IsBlocked("b" + ipAddrInternal)
	}

	displayPosts := make([]*PostDisplay, 0)
	for _, p := range posts {
		pd := NewPostDisplay(p, forum, isAdmin)
		if pd != nil {
			displayPosts = append(displayPosts, pd)
		}
	}

	model := struct {
		Forum
		Posts       []*PostDisplay
		TotalCount  int
		IsAdmin     bool
		User        string
		IPAddr      string
		IPAddrI32   string
		IPAddrI24   string
		IPAddrI20   string
		IPAddrI16   string
		Blocked     map[string]string
		IPBlocked   bool
		UserBlocked bool
		LogInOut    template.HTML
	}{
		Forum:       *forum,
		Posts:       displayPosts,
		TotalCount:  total,
		IsAdmin:     isAdmin,
		User:        userInternal,
		IPAddr:      ipAddr,
		IPAddrI32:   ipAddrInternal,
		UserBlocked: userBlocked,
		IPBlocked:   ipBlocked,
		LogInOut:    getLogInOut(r, getSecureCookie(r)),
	}

	if len(ipAddrInternal) == 8 {
		model.IPAddrI24 = ipAddrInternal[:6]
		model.IPAddrI20 = ipAddrInternal[:5]
		model.IPAddrI16 = ipAddrInternal[:4]
	}

	if isAdmin {
		model.Blocked = make(map[string]string)
		for k := range forum.Store.blocked {
			if k[0] == 'b' {
				model.Blocked[k[1:]] = ipAddrInternalToOriginal(k[1:])
			}
		}
	}

	ExecTemplate(w, tmplPosts, model)
}
