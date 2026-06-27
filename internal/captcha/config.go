package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"glm5.2proxy/internal/config"
)

type AliyunConfig struct {
	Enabled bool   `json:"enabled"`
	Region  string `json:"region"`
	Prefix  string `json:"prefix"`
	SceneID string `json:"sceneId"`
}

func FetchConfig(ctx context.Context, cfg config.Config) (AliyunConfig, error) {
	endpoint, _ := url.Parse("https://zcode.z.ai/api/v1/client/configs")
	query := endpoint.Query()
	query.Set("app_version", cfg.AppVersion)
	query.Set("platform", cfg.Platform)
	endpoint.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return AliyunConfig{}, err
	}
	defer response.Body.Close()
	var body struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Configs struct {
				Captcha AliyunConfig `json:"captcha"`
			} `json:"configs"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return AliyunConfig{}, err
	}
	captcha := body.Data.Configs.Captcha
	if response.StatusCode != http.StatusOK || body.Code != 0 {
		return AliyunConfig{}, fmt.Errorf("captcha config failed: HTTP %d %s", response.StatusCode, body.Msg)
	}
	if !captcha.Enabled || captcha.Region == "" || captcha.Prefix == "" || captcha.SceneID == "" {
		return AliyunConfig{}, errors.New("ZCode returned incomplete or disabled captcha configuration")
	}
	return captcha, nil
}
