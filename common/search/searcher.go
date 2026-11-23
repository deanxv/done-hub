package search

import (
	"done-hub/common/logger"
	"done-hub/common/search/channel"
	"done-hub/common/search/search_type"

	"github.com/spf13/viper"
)

type Searcher interface {
	Query(query string) (*search_type.SearchResponses, error)
	Name() string
}

func InitSearcher() {
	InitSearxng()
	InitTavily()
	InitKeenable()
}

func InitSearxng() {
	searxngUrl := viper.GetString("search.searxng.url")
	if searxngUrl == "" {
		logger.SysLog("searxng url is empty")
		return
	}

	searxng := channel.NewSearxng(searxngUrl)
	AddSearchers(searxng)
}

func InitTavily() {
	tavilyKey := viper.GetString("search.tavily.key")
	if tavilyKey == "" {
		logger.SysLog("tavily key is empty")
		return
	}

	tavily := channel.NewTavily(tavilyKey)
	AddSearchers(tavily)
}

func InitKeenable() {
	keenableKey := viper.GetString("search.keenable.key")
	if keenableKey == "" {
		logger.SysLog("keenable key is empty")
		return
	}

	keenable := channel.NewKeenable(keenableKey, viper.GetString("search.keenable.base_url"))
	AddSearchers(keenable)
}
