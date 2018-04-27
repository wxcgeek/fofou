// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testIPConvOne(t *testing.T, s string) {
	internal := ipAddrToInternal(s)
	orig := ipAddrInternalToOriginal(internal)
	if s != orig {
		t.Fatalf("%q != %q", s, orig)
	}
}

func testMakeInternalUserName(t *testing.T, given string, twitter bool, expected string) {
	res := MakeInternalUserName(given, twitter)
	if res != expected {
		t.Fatalf("%q != %q", res, expected)
	}
}

func testipAddrFromRemoteAddr(t *testing.T, s, expected string) {
	res := ipAddrFromRemoteAddr(s)
	if res != expected {
		t.Fatalf("%q != %q", res, expected)
	}
}

func testStringSliceEq(t *testing.T, s1, s2 []string) {
	if len(s1) != len(s2) {
		t.Fatalf("len(s1) != len(s2) (%d != %d)", len(s1), len(s2))
	}
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("s1[%d] != s2[%d] (%s != %s)", i, i, s1[i], s2[i])
		}
	}
}

func TestIpConv(t *testing.T) {
	testIPConvOne(t, "127.0.0.1")
	testIPConvOne(t, "27.3.255.238")
	testIPConvOne(t, "hello kitty")

	testMakeInternalUserName(t, "foo", false, "foo")
	testMakeInternalUserName(t, "foo", true, "t:foo")
	testMakeInternalUserName(t, "t:foo", false, "foo")
	testMakeInternalUserName(t, "p:", false, "p")

	testipAddrFromRemoteAddr(t, "foo", "foo")
	testipAddrFromRemoteAddr(t, "[::1]:58292", "[::1]")
	testipAddrFromRemoteAddr(t, "127.0.0.1:856", "127.0.0.1")

	a := []string{"foo", "bar", "go"}
	deleteStringIn(&a, "foo")
	testStringSliceEq(t, a, []string{"go", "bar"})
	deleteStringIn(&a, "go")
	testStringSliceEq(t, a, []string{"bar"})
	deleteStringIn(&a, "baro")
	testStringSliceEq(t, a, []string{"bar"})
	deleteStringIn(&a, "bar")
	testStringSliceEq(t, a, []string{})
}

func testUnCaps(t *testing.T, s, exp string) {
	got := UnCaps(s)
	if got != exp {
		t.Fatalf("\n%#v !=\n%#v (for '%#v')", got, exp, s)
	}
}

func TestUnCaps(t *testing.T) {
	d := []string{
		"FOO", "Foo",
		//"FOO BAR. IS IT ME?\nOR ME", "Foo bar. Is it me?\nOr me",
	}
	for i := 0; i < len(d)/2; i++ {
		testUnCaps(t, d[i*2], d[i*2+1])
	}
}

func TestPost(t *testing.T) {
	var count int64

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns: 1000,
		},
	}

	post := func(wg *sync.WaitGroup) {

		v := url.Values{}
		v.Add("Name", "zzz")
		v.Add("Message", time.Now().Format(time.RFC1123))
		v.Add("Subject", time.Now().Format(time.RFC1123))
		v.Add("Cancel", "")
		req, _ := http.NewRequest("POST", "http://127.0.0.1:5010/sumatrapdf/newpost?"+v.Encode(), nil)
		req.Header.Set("Content-Type", "application/www-x-form-urlencoded")
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == 200 {
				atomic.AddInt64(&count, 1)
			}
			resp.Body.Close()
		} else {
			os.Stderr.WriteString(err.Error() + "\n")
		}
		wg.Done()
	}

	get := func(wg *sync.WaitGroup) {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:5010/sumatrapdf/newpost?from="+strconv.Itoa(rand.Intn(10000)), nil)
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == 200 {
				atomic.AddInt64(&count, 1)
			}
			resp.Body.Close()
		} else {
			os.Stderr.WriteString(err.Error() + "\n")
		}
		wg.Done()
	}

	_, _ = post, get
	for i := 0; i < 1000; i++ {
		wg := &sync.WaitGroup{}
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go post(wg)
		}

		wg.Wait()
		os.Stderr.WriteString(strconv.Itoa(i) + " " + strconv.FormatInt(count, 10) + "\n")
	}
	t.Error(1)
}
