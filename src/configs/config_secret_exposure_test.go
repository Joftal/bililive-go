package configs

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

// 斗鱼长期凭证只能落盘，不能进 JSON：GET /api/config 直接 writeJSON(整个 Config)，
// 一旦 LTP0/DyDid 带 json tag，数月有效的 passport 域凭证就会被下发到浏览器。
// 同时 YAML 侧必须保留，否则重启/续期后拿不到 LTP0，自动续期整体失效。
// 注意：GET /api/raw-config（设置页的 YAML 明文）与本用例无关，它仍会返回配置文件原文，
// 与 cookies / sooplive_auth 的既有暴露面相同。
func TestDouyuAuthSecretsStayOutOfJSON(t *testing.T) {
	c := &Config{
		DouyuAuth: DouyuAuth{
			LTP0:          "long-term-passport-ticket",
			DyDid:         "device-id-value",
			NextRefreshAt: 1893456000,
			NeedRescan:    true,
		},
	}

	b, err := json.Marshal(c)
	assert.NoError(t, err)
	assert.NotContains(t, string(b), "long-term-passport-ticket")
	assert.NotContains(t, string(b), "device-id-value")
	assert.NotContains(t, string(b), `"ltp0"`)
	// 非敏感状态字段仍要可见，前端续期健康度提示依赖它们
	assert.Contains(t, string(b), `"need_rescan":true`)
	assert.Contains(t, string(b), `"next_refresh_at":1893456000`)

	y, err := yaml.Marshal(c)
	assert.NoError(t, err)
	assert.Contains(t, string(y), "long-term-passport-ticket")
	assert.Contains(t, string(y), "device-id-value")
}
