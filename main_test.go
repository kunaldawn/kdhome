package main

import "testing"

func TestIsBot(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want bool
	}{
		// real browsers → human
		{"chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36", false},
		{"firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0", false},
		{"ios_safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1", false},
		// crawlers caught by the generic "bot" token
		{"gptbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.1; +https://openai.com/gptbot", true},
		{"claudebot", "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)", true},
		{"ahrefsbot", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", true},
		{"uptimerobot", "Mozilla/5.0+(compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)", true},
		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true},
		// caught by "spider" / "slurp"
		{"bytespider", "Mozilla/5.0 (Linux; Android 5.0) AppleWebKit/537.36 (KHTML, like Gecko) Mobile Safari/537.36 (compatible; Bytespider; spider-feedback@bytedance.com)", true},
		{"slurp", "Mozilla/5.0 (compatible; Yahoo! Slurp; http://help.yahoo.com/help/us/ysearch/slurp)", true},
		// specific tokens NOT subsumed by a generic token (regression guards)
		{"curl", "curl/8.7.1", true},
		{"wget", "Wget/1.21.3", true},
		{"python_requests", "python-requests/2.31.0", true},
		{"go_http", "Go-http-client/2.0", true},
		{"java", "Java/11.0.2", true},
		{"headlesschrome", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/112.0.0.0 Safari/537.36", true},
		{"facebookexternalhit", "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)", true},
		// no User-Agent at all
		{"empty", "", true},
		{"whitespace", "   ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isBot(c.ua); got != c.want {
				t.Errorf("isBot(%q) = %v, want %v", c.ua, got, c.want)
			}
		})
	}
}
