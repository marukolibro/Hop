package service

import (
	"strings"
	"unicode"

	"hop/internal/model"
)

// serviceRule names the keywords that say a group routes one kind of
// service. Providers name their groups differently ("Netflix", "奈飞",
// "国外媒体", "GlobalMedia"), so the rules know the common names in English
// and Chinese; a group no rule knows simply gets no service.
type serviceRule struct {
	service  string
	keywords []string
}

// serviceRules are tried in order, so a specific service comes before the
// general kind it belongs to ("steamcn" is a game, not "cn" direct).
var serviceRules = []serviceRule{
	{model.ServiceAdBlock, []string{"广告", "拦截", "屏蔽", "隐私", "adblock", "adguard", "reject", "block", "privacy", "ads"}},
	{model.ServiceAI, []string{"openai", "chatgpt", "claude", "anthropic", "gemini", "copilot", "perplexity", "deepseek", "grok", "人工智能", "ai"}},
	{model.ServiceTelegram, []string{"telegram", "电报", "tg"}},
	{model.ServiceGame, []string{"steam", "epic", "playstation", "xbox", "nintendo", "blizzard", "battle.net", "riot", "游戏", "game", "psn"}},
	{model.ServiceMusic, []string{"spotify", "netease", "tidal", "soundcloud", "音乐", "music"}},
	{model.ServiceVideo, []string{"netflix", "奈飞", "disney", "youtube", "油管", "hbo", "primevideo", "hulu", "bilibili", "哔哩", "tiktok", "抖音", "emby", "twitch", "dazn", "iqiyi", "爱奇艺", "bahamut", "巴哈", "viu", "abema", "媒体", "视频", "直播", "media", "video", "stream"}},
	{model.ServiceSearch, []string{"google", "谷歌", "bing", "duckduckgo", "搜索", "search"}},
	{model.ServiceApple, []string{"apple", "icloud", "itunes", "appstore", "苹果"}},
	{model.ServiceMicrosoft, []string{"microsoft", "微软", "windows", "office", "onedrive", "msn"}},
	{model.ServiceChat, []string{"whatsapp", "discord", "signal", "wechat", "微信", "messenger", "slack", "skype", "zoom", "聊天", "chat", "line"}},
	{model.ServiceSocial, []string{"twitter", "facebook", "instagram", "reddit", "weibo", "微博", "linkedin", "pinterest", "tumblr", "threads", "社交", "social", "meta"}},
	{model.ServiceCloud, []string{"dropbox", "网盘", "cloudflare", "aws", "azure", "cloud", "drive"}},
	{model.ServiceDev, []string{"github", "gitlab", "docker", "stackoverflow", "jetbrains", "npm", "开发", "developer", "dev", "code"}},
	{model.ServiceShop, []string{"amazon", "taobao", "ebay", "paypal", "shopee", "aliexpress", "购物", "shop"}},
	{model.ServiceDownload, []string{"torrent", "tracker", "迅雷", "下载", "download", "pt", "bt"}},
	{model.ServiceDirect, []string{"直连", "国内", "大陆", "局域网", "direct", "china", "domestic", "bypass", "local", "lan", "cn"}},
	{model.ServiceFinal, []string{"漏网", "兜底", "final", "match", "default", "others", "其他"}},
}

// shortKeywordLen is the length up to which an ASCII keyword has to be a
// word of the name by itself: "ai" must not match "mail", nor "pt" "script".
const shortKeywordLen = 3

// ServiceOf says what kind of service a group named name routes, or "" when
// no rule knows it.
func ServiceOf(name string) string {
	lower := strings.ToLower(name)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, rule := range serviceRules {
		for _, keyword := range rule.keywords {
			if matchesKeyword(lower, words, keyword) {
				return rule.service
			}
		}
	}
	return ""
}

func matchesKeyword(lower string, words []string, keyword string) bool {
	if !isASCII(keyword) || len(keyword) > shortKeywordLen {
		return strings.Contains(lower, keyword)
	}
	for _, word := range words {
		if word == keyword {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}
