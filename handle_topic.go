// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"strconv"

	"github.com/coyove/fofou/server"
	"github.com/kjk/u"
)

// url: /{forum}/topic?id=${id}
func handleTopic(w http.ResponseWriter, r *http.Request) {
	topicID, _ := strconv.Atoi(r.FormValue("id"))
	topic := forum.Store.TopicByID(uint32(topicID))
	if nil == topic {
		path := forum.Store.BuildArchivePath(uint32(topicID))
		if u.PathExists(path) {
			var err error
			topic, err = LoadSingleTopicInStore(path)
			if err == nil {
				topic.Archived = true
				goto NEXT
			}
		}

		forum.Notice("handleTopic(): didn't find topic with id %d, referer: %q", topicID, r.Referer())
		http.Redirect(w, r, "/", 302)
		return
	}
NEXT:
	isAdmin := forum.IsAdmin(getUser(r).ID)
	if topic.IsDeleted() && !isAdmin {
		http.Redirect(w, r, "/", 302)
		return
	}

	model := struct {
		*Forum
		*Topic
		TopicID int
		IsAdmin bool
	}{
		Forum:   forum,
		Topic:   topic,
		TopicID: topicID,
		IsAdmin: isAdmin,
	}
	server.Render(w, server.TmplTopic, model)
}
