package gohttp

import (
	"net/url"
	"regexp"
)

var (
	parmsReg    = regexp.MustCompile(`name="(s|jschl_vc|pass)"(?: [^<>]*)? value="(.+?)"`)
	evalJs      = regexp.MustCompile(`setTimeout\(function\(\){\s*(var s,t,o,p,b,r,e,a,k,i,n,g,f.+?\r?\n[\s\S]+?a\.value\s*=.+?)\r?\n(?:[^{<>]*},\s*(\d{4,}))?`)
	innerHtml   = regexp.MustCompile(`<div(?: [^<>]*)? id=\"cf-dn.*?\">([^<>]*)`)
	nnJs        = regexp.MustCompile(`2;url=(.*[0-9])`)
	jschlRegexp = regexp.MustCompile(`name="jschl_vc" value="(\w+)"`)
	passRegexp  = regexp.MustCompile(`name="pass" value="(.+?)"`)
)

func byPassCloudFlare(response string) {
	var params = make(url.Values)

	if m := jschlRegexp.FindStringSubmatch(string(response)); len(m) > 0 {
		params.Set("jschl_vc", m[1])
	}

	if m := passRegexp.FindStringSubmatch(string(response)); len(m) > 0 {
		params.Set("pass", m[1])
	}

}
