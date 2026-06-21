package model

import (
	"crypto/md5"
	"done-hub/common/config"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SearchChannelsTagParams struct {
	Tag string `json:"tag" form:"tag"`
	PaginationParams
}

type ChannelTag struct {
	ID           int     `json:"id"`
	Tag          string  `json:"tag"`
	Count        int     `json:"count"`         // 该标签下的渠道总数
	Enabled      int     `json:"enabled"`       // 其中已启用的渠道数
	TypeCount    int     `json:"type_count"`    // 组内不同渠道类型数，>1 表示类型不一致（混合）
	GroupCount   int     `json:"group_count"`   // 组内不同分组配置数，>1 表示分组不一致（混合）
	UsedQuota    int64   `json:"used_quota"`    // 组内已用额度合计
	Balance      float64 `json:"balance"`       // 组内余额合计
	ResponseTime float64 `json:"response_time"` // 组内已测渠道的平均响应时间(ms)，未测渠道不计入
}

// CheckTagTypeConsistency 护栏：标签语义是「同配置、多 Key 的克隆组」，全组类型必须一致。
// 阻止把不同类型的渠道加入同一标签，避免后续分组统一编辑把整组覆盖成单一类型。
// excludeID 用于编辑自身时排除当前渠道（新建时传 0）。
func CheckTagTypeConsistency(tag string, channelType int, excludeID int) error {
	if tag == "" {
		return nil
	}
	var types []int
	err := DB.Model(&Channel{}).
		Distinct("type").
		Where("tag = ? AND id <> ?", tag, excludeID).
		Pluck("type", &types).Error
	if err != nil {
		return err
	}
	for _, t := range types {
		if t != channelType {
			return fmt.Errorf("标签「%s」下已有其它类型的渠道，无法加入不同类型的渠道；请使用相同类型，或换一个标签名", tag)
		}
	}
	return nil
}

func GetChannelsTagList(params *SearchChannelsTagParams) (*DataResult[Channel], error) {
	var channels []*Channel
	// 子表格需逐行管理 key，故不 Omit("key")
	db := DB.Where("tag = ?", params.Tag)
	return PaginateAndOrder(db, &params.PaginationParams, &channels, allowedChannelOrderFields)
}

func GetChannelsTagAllList() ([]*ChannelTag, error) {
	var channelTags []*ChannelTag
	groupField := quotePostgresField("group")
	err := DB.Model(&Channel{}).
		Select("tag, COUNT(*) as count, "+
			"SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) as enabled, "+
			"COUNT(DISTINCT type) as type_count, "+
			"COUNT(DISTINCT "+groupField+") as group_count, "+
			"SUM(used_quota) as used_quota, "+
			"SUM(balance) as balance, "+
			"AVG(CASE WHEN response_time > 0 THEN response_time ELSE NULL END) as response_time", config.ChannelStatusEnabled).
		Where("tag != ''").
		Group("tag").
		Find(&channelTags).Error

	return channelTags, err
}

type ChannelTagCollection struct {
	Channel
	KeyMap map[string]int
	Count  int `json:"count"` // 该标签下的渠道总数（含禁用），用于前端"覆盖全部 N 个"提示
}

func GetChannelsTag(tag string) (*ChannelTagCollection, error) {
	var channelTag ChannelTagCollection

	var channels []Channel
	err := DB.Where("tag = ?", tag).Order("id ASC").Find(&channels).Error
	if err != nil {
		return nil, err
	}

	if len(channels) == 0 {
		return nil, errors.New("tag不存在")
	}

	channelTag.Channel = channels[0]
	channelTag.Count = len(channels)
	channelTag.Key = ""

	channelTag.KeyMap = make(map[string]int)
	for _, c := range channels {
		keyMd5 := md5.Sum([]byte(c.Key))
		keyMd5Str := hex.EncodeToString(keyMd5[:])
		channelTag.KeyMap[keyMd5Str] = c.Id
		channelTag.Key += c.Key + "\n"
	}

	channelTag.Key = strings.TrimRight(channelTag.Key, "\n")
	return &channelTag, nil
}

func UpdateChannelsTag(tag string, channel *Channel) error {
	channelTag, err := GetChannelsTag(tag)
	if err != nil {
		return err
	}

	if channel.Key == "" {
		return errors.New("key不能为空")
	}

	// tag 是分组的唯一标识，若清空会因 Select("*") 强制写入而意外解散整组
	if channel.Tag == "" {
		return errors.New("tag不能为空")
	}

	addKeys := []string{}
	delIds := []int{}

	newKeysMap := make(map[string]bool)

	keys := strings.Split(channel.Key, "\n")
	for _, key := range keys {
		if key == "" {
			continue
		}
		keyMd5 := md5.Sum([]byte(key))
		keyMd5Str := hex.EncodeToString(keyMd5[:])
		newKeysMap[keyMd5Str] = true

		// 如果key不在现有的KeyMap中，则添加到addKeys
		if _, ok := channelTag.KeyMap[keyMd5Str]; !ok {
			addKeys = append(addKeys, key)
		}
	}

	// 检查现有的keys，如果不在新的keys中，则需要删除
	for keyMd5Str, id := range channelTag.KeyMap {
		if _, ok := newKeysMap[keyMd5Str]; !ok {
			delIds = append(delIds, id)
		}
	}

	tx := DB.Begin()
	// 先处理要删除的数据
	if len(delIds) > 0 {
		err = tx.Where("id IN (?)", delIds).Delete(&Channel{}).Error
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 处理要添加的数据
	if len(addKeys) > 0 {
		maxKey := len(channelTag.KeyMap)

		addChannels := make([]Channel, 0, len(addKeys))
		for _, key := range addKeys {
			addChannel := *channel
			addChannel.Name = fmt.Sprintf("%s_%d", channel.Name, maxKey)
			addChannel.Key = key
			addChannel.Status = config.ChannelStatusEnabled
			addChannel.Balance = 0
			addChannel.BalanceUpdatedTime = 0
			addChannel.UsedQuota = 0
			addChannel.ResponseTime = 0
			addChannel.CreatedTime = time.Now().Unix()
			addChannel.TestTime = 0
			addChannels = append(addChannels, addChannel)
			maxKey++
		}
		err = BatchInsert(tx, addChannels)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 用 Select("*") + Omit 黑名单覆盖共享配置：
	//  - Select("*") 强制写入零值（修复 only_chat=false / pre_cost=0 / other="" 等无法保存的问题）
	//  - 黑名单排除逐行/运行时字段；新增 Channel 字段会自动纳入批量更新，避免漏字段
	//  - priority/weight/cost_ratio 为逐行可调字段（成本倍率支持组内各渠道单独设置），不随分组统一编辑覆盖
	//  - type 为克隆组的根本属性，由创建时决定；分组编辑弹窗不显示类型字段，故统一编辑不得改写 type，
	//    否则会把代表渠道的类型悄悄覆盖到全组（混合组尤其危险），违反「所见即所得」
	err = tx.Model(&Channel{}).Where("tag = ?", tag).
		Select("*").
		Omit("id", "key", "type", "status", "priority", "weight", "cost_ratio",
			"used_quota", "balance", "balance_updated_time",
			"response_time", "created_time", "test_time", "name", "deleted_at").
		Updates(channel).Error

	if err != nil {
		tx.Rollback()
		return err
	}

	tx.Commit()

	ChannelGroup.Load()

	return err
}

func DeleteChannelsTag(tag string, delDisabled bool) error {
	if tag == "" {
		return nil
	}

	// 单条 Delete 本身原子，无需显式事务；条件从 DB.Where 起新建，避免污染共享句柄
	query := DB.Where("tag = ?", tag)
	if delDisabled {
		query = query.Where("status IN ?", []int{config.ChannelStatusAutoDisabled, config.ChannelStatusManuallyDisabled})
	}

	result := query.Delete(&Channel{})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		ChannelGroup.Load()
	}

	return nil
}

func ChangeChannelsTagStatus(tag string, status int) error {
	if tag == "" {
		return nil
	}

	result := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", status)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		ChannelGroup.Load()
	}

	return nil
}

func UpdateChannelsTagPriority(tag string, value int) error {
	result := DB.Model(&Channel{}).Where("tag = ?", tag).Update("priority", value)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		ChannelGroup.Load()
	}
	return nil
}

// UpdateChannelsTagWeight 将整组渠道的权重统一设为同一值（权重为逐行字段，需专用入口批量设置）。
func UpdateChannelsTagWeight(tag string, value uint) error {
	result := DB.Model(&Channel{}).Where("tag = ?", tag).Update("weight", value)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		ChannelGroup.Load()
	}
	return nil
}

// UpdateChannelsTagCostRatio 将整组渠道的成本倍率统一设为同一值。
func UpdateChannelsTagCostRatio(tag string, value float64) error {
	result := DB.Model(&Channel{}).Where("tag = ?", tag).Update("cost_ratio", value)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		ChannelGroup.Load()
	}
	return nil
}
