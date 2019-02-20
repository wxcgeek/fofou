// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kjk/u"
)

var stdTimeFormat = "2006-01-02 15:04:05"

type PostDisplay struct {
	Post
	ActualNo     int
	UserHomepage string
	MessageHtml  template.HTML
	CssClass     string
}

func prettySec(sec uint32) string {
	t := time.Unix(int64(sec), 0)
	d := time.Now().Sub(t)
	if sec := d.Seconds(); sec < 60 {
		return fmt.Sprintf("~ %.0f s", sec)
	} else if sec < 3600 {
		return fmt.Sprintf("~ %.0f mins", sec/60)
	} else if sec < 86400 {
		return fmt.Sprintf("~ %.1f hrs", sec/3600)
	} else if sec < 86400*7 {
		return fmt.Sprintf("~ %.1f days", sec/86400)
	}
	return "@ " + t.Format(stdTimeFormat)
}

func (p *PostDisplay) CreatedOnStr() string {
	return prettySec(p.CreatedAt)
}

func NewPostDisplay(p Post, forum *Forum, isAdmin bool) *PostDisplay {
	if p.IsDeleted && !isAdmin {
		return nil
	}

	pd := &PostDisplay{
		Post:     p,
		CssClass: "post",
	}
	if p.IsDeleted {
		pd.CssClass = "post deleted"
	}
	msgHtml := msgToHtml(p.Message)
	pd.MessageHtml = template.HTML(msgHtml)

	if p.IsGithubUser() {
		pd.UserHomepage = "https://github.com/" + p.UserName()
	}

	if forum.ForumUrl == "sumatrapdf" {
		// backwards-compatibility hack for posts imported from old version of
		// fofou: hyper-link my name to my website
		if p.UserName() == "Krzysztof Kowalczyk" {
			pd.UserHomepage = "http://blog.kowalczyk.info"
		}
	}
	return pd
}

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

func getLogInOut(r *http.Request, c *SecureCookieValue) template.HTML {
	return template.HTML(c.GithubUser)
}

// url: /{forum}/topic?id=${id}
func handleTopic(w http.ResponseWriter, r *http.Request) {
	forum := mustGetForum(w, r)
	if forum == nil {
		return
	}
	idStr := strings.TrimSpace(r.FormValue("id"))
	topicID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}

	//fmt.Printf("handleTopic(): forum: %q, topicId: %d\n", forum.ForumUrl, topicId)
	topic := forum.Store.TopicByID(uint32(topicID))
	if nil == topic {
		path := forum.Store.BuildArchivePath(uint32(topicID))
		if u.PathExists(path) {
			topic, err = LoadSingleTopicInStore(path)
			if err == nil {
				topic.Archived = true
				goto NEXT
			}
		}

		logger.Noticef("handleTopic(): didn't find topic with id %d, referer: %q", topicID, getReferer(r))
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}
NEXT:

	isAdmin := userIsAdmin(forum, getSecureCookie(r))
	if topic.IsDeleted() && !isAdmin {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}

	posts := make([]*PostDisplay, 0, len(topic.Posts))
	for no, p := range topic.Posts {
		pd := NewPostDisplay(p, forum, isAdmin)
		if pd != nil {
			pd.ActualNo = no + 1
			posts = append(posts, pd)
		}
	}

	model := struct {
		Forum
		Topic
		Posts         []*PostDisplay
		IsAdmin       bool
		AnalyticsCode *string
		LogInOut      template.HTML
	}{
		Forum:    *forum,
		Topic:    *topic,
		Posts:    posts,
		IsAdmin:  isAdmin,
		LogInOut: getLogInOut(r, getSecureCookie(r)),
	}
	ExecTemplate(w, tmplTopic, model)
}
