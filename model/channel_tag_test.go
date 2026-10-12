package model

import (
	"testing"

	"done-hub/common/logger"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 分组统一编辑不得覆盖逐渠道的 remark，新增 key 生成的渠道 remark 为空；共享配置（如 need2response_models）应同步到全组。
func TestUpdateChannelsTagKeepsPerChannelRemark(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Channel{}); err != nil {
		t.Fatal(err)
	}
	oldDB, oldLogger := DB, logger.Logger
	DB = db
	// UpdateChannelsTag 末尾 ChannelGroup.Load() 会打日志，测试环境未 SetupLogger
	logger.Logger = zap.NewNop()
	defer func() { DB, logger.Logger = oldDB, oldLogger }()

	seed := []Channel{
		{Name: "a", Key: "k1", Tag: "g", Remark: "remark-a", Status: 1},
		{Name: "b", Key: "k2", Tag: "g", Remark: "remark-b", Status: 1},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}

	routes := "gpt-5-pro"
	edit := seed[0]
	edit.Key = "k1\nk2\nk3"
	edit.Remark = "overwritten"
	edit.Need2ResponseModels = &routes
	if err := UpdateChannelsTag("g", &edit); err != nil {
		t.Fatal(err)
	}

	var got []Channel
	db.Order("id").Find(&got)
	if len(got) != 3 {
		t.Fatalf("want 3 channels, got %d", len(got))
	}
	for i, want := range []string{"remark-a", "remark-b", ""} {
		if got[i].Remark != want {
			t.Errorf("channel %d remark = %q, want %q", got[i].Id, got[i].Remark, want)
		}
		if got[i].Need2ResponseModels == nil || *got[i].Need2ResponseModels != routes {
			t.Errorf("channel %d need2response_models not synced: %v", got[i].Id, got[i].Need2ResponseModels)
		}
	}
}
