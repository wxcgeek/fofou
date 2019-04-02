package markup

import "testing"

func assert(a string, f bool, b string) {
	test = true
	if x := Do(a, f, 0); x != b {
		if f {
			panic("\nsrc(h): " + a + "\nout: " + x + "\nexpect: " + b)
		}
		panic("\nsrc: " + a + "\nout: " + x + "\nexpect: " + b)
	}
}

func TestDoCode(t *testing.T) {
	assert("```a``````b```", true, "<code>a</code><code>b</code>")
	assert("```a``````b```", false, "<code>a</code><code>b</code>")
	assert("```a`````b```", true, "<code>a</code>``b<code></code>")
	assert("````a`````b```", true, "a`b<code></code>")
	assert("````a`````b```", false, "<code>`a</code>``b<code></code>")
	assert("```>>1```", false, "<code>&gt;&gt;1</code>")
	assert("```====```", false, "<code>====</code>")
	assert("``a`````<b>`", true, "``a`<b>`")
	assert("``a`````<b>`", false, "``a<code>``&lt;b&gt;`</code>")
	assert("```[http```bc]", true, "<code>[http</code>bc]")
	assert("```[http]```bc]", true, "<code>[http]</code>bc]")
	assert("[ab```c]```", true, "[ab<code>c]</code>")
	assert("[ab```c```]", true, "[ab<code>c</code>]")
}

func TestDo(t *testing.T) {
	assert("abc", true, "abc")
	assert("abc\ndef", true, "abc<br>def")
	assert("abc\n>>def", true, "abc<br>&gt;&gt;def")
	assert("abc\n>>1.2", true, "abc<br>#TEST#1#TEST#.2")
	assert("abc\n>>1", true, "abc<br>#TEST#1#TEST#")
	assert("abc\n===", true, "abc<br>===")
	assert("abc\n====", true, "abc<br><hr>")
	assert("abc\n`====`", true, "abc<br>`<hr>`")
	assert("[abc]", true, "[abc]")
	assert("a[http://a.com]b", true, "a#TEST#http://a.com#TEST#b")
	assert("[ab[c]]", true, "[ab[c]]")
	assert("[ab[cd[http://a]ef]gh]", true, "[ab[cd#TEST#http://a#TEST#ef]gh]")
	assert("a]b", true, "a]b")
	assert("[abc", true, "[abc")
	assert("[abc`", true, "[abc`")
	assert("[abc`d]", true, "[abc`d]")
	assert("[htt`p]", true, "[htt`p]")
	assert("[<a>a</a>]", true, "[&lt;a&gt;a&lt;/a&gt;]")
	assert("[<a>[a]</a>]", true, "[&lt;a&gt;[a]&lt;/a&gt;]")
	assert("[<a>[a</a>]]", true, "[&lt;a&gt;[a&lt;/a&gt;]]")
	assert("[a====b]", true, "[a<hr>b]")
}
