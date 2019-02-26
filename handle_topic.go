// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/kjk/u"
)

// TODO: this is simplistic but work for me, http://net.tutsplus.com/tutorials/other/8-regular-expressions-you-should-know/
// has more elaborate regex for extracting urls
var urlRx = regexp.MustCompile(`(https?://[[:^space:]]+|<|\n| |` + "```[\\s\\S]+```" + `)`)

func msgToHtml(s string) string {
	return urlRx.ReplaceAllStringFunc(s, func(in string) string {
		switch in {
		case " ":
			return "&nbsp;"
		case "\n":
			return "<br>"
		case "<":
			return "&lt;"
		default:
			if strings.HasPrefix(in, "```") {
				return "<code>" + strings.Replace(in[3:len(in)-3], "<", "&lt;", -1) + "</code>"
			} else if strings.HasSuffix(in, ".png") || strings.HasSuffix(in, ".jpg") || strings.HasSuffix(in, ".gif") {
				return "<img class=image alt='" + in + "' src='" + in + "'/>"
			} else {
				return "<a href='" + in + "' target=_blank>" + in + "</a>"
			}
		}
	})
}

// url: /{forum}/topic?id=${id}
func handleTopic(forum *Forum, topicID int, w http.ResponseWriter, r *http.Request) {
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

		logger.Noticef("handleTopic(): didn't find topic with id %d, referer: %q", topicID, getReferer(r))
		http.Redirect(w, r, "/", 302)
		return
	}
NEXT:
	isAdmin := forum.IsAdmin(getSecureCookie(r))
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
	ExecTemplate(w, tmplTopic, model)
}
