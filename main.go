package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// MiaoConfig 配置文件结构体
type MiaoConfig struct {
	Cookie string `json:"cookie"`
}

// 读取配置文件
func readConfig(filename string) (*MiaoConfig, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config MiaoConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// 获取请求头
func getHeaders(config *MiaoConfig) map[string]string {
	return map[string]string{
		"accept":             "application/json, text/plain, */*",
		"accept-language":    "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
		"cache-control":      "no-cache",
		"cookie":             config.Cookie,
		"dnt":                "1",
		"origin":             "https://www.bilibili.com",
		"pragma":             "no-cache",
		"priority":           "u=1, i",
		"referer":            "https://www.bilibili.com/video/BV1wg4y127mJ/?spm_id_from=333.337.search-card.all.click&vd_source=f4b11eff4d5b11ae41cb4e0ca94e674b",
		"sec-ch-ua":          "\"Chromium\";v=\"128\", \"Not;A=Brand\";v=\"24\", \"Microsoft Edge\";v=\"128\"",
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": "\"Windows\"",
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "same-site",
		"user-agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36 Edg/128.0.0.0",
	}
}

// sanitizeFileName 清理文件名中的非法字符
func sanitizeFileName(name string) string {
	reg := regexp.MustCompile(`[<>:"/\\|?*]+`)
	return reg.ReplaceAllString(name, "_")
}

// VideoInfo 视频信息
type VideoInfo struct {
	Cid      string `json:"cid"`
	PartName string `json:"partName"`
}

// getVideoInfo 获取视频信息列表
func getVideoInfo(bvid string, headers map[string]string) ([]VideoInfo, error) {
	url := fmt.Sprintf("https://api.bilibili.com/x/player/pagelist?bvid=%s", bvid)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Cid  int    `json:"cid"`
			Part string `json:"part"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var infoList []VideoInfo
	for _, v := range result.Data {
		infoList = append(infoList, VideoInfo{
			Cid:      strconv.Itoa(v.Cid),
			PartName: sanitizeFileName(v.Part),
		})
	}

	return infoList, nil
}

// getAudioUrl 获取音频地址
func getAudioUrl(bvid string, cid string, headers map[string]string) (string, error) {
	url := "https://api.bilibili.com/x/player/wbi/playurl"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	q := req.URL.Query()
	q.Add("fnval", "4048")
	q.Add("bvid", bvid)
	q.Add("cid", cid)
	req.URL.RawQuery = q.Encode()

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Dash struct {
				Audio []struct {
					BaseUrl string `json:"baseUrl"`
				} `json:"audio"`
			} `json:"dash"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Data.Dash.Audio[0].BaseUrl, nil
}

// getFileStream 获取文件流
func getFileStream(url string, headers map[string]string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// convertToMp3Stream 将音频流转换为mp3并保存
func convertToMp3Stream(inputStream io.Reader, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	cmd := exec.Command("ffmpeg", "-i", "pipe:0", "-q:a", "0", outputPath)
	cmd.Stdin = inputStream
	return cmd.Run()
}

func main() {
	// 读取配置文件
	config, err := readConfig("./miaoConfig.json")
	if err != nil {
		panic(err)
	}

	// 获取请求头
	headers := getHeaders(config)

	// 从控制台读取bvid
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("请输入BVID: ")
	bvid, _ := reader.ReadString('\n')
	bvid = strings.TrimSpace(bvid)

	infoList, err := getVideoInfo(bvid, headers)
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	ch := make(chan error, len(infoList)) // 用于传递错误信息的channel

	for _, info := range infoList {
		wg.Add(1)
		go func(info VideoInfo) {
			defer wg.Done()
			audioUrl, err := getAudioUrl(bvid, info.Cid, headers)
			if err != nil {
				ch <- fmt.Errorf("获取音频地址失败: %w", err)
				return
			}

			audioStream, err := getFileStream(audioUrl, headers)
			if err != nil {
				ch <- fmt.Errorf("获取音频流失败: %w", err)
				return
			}
			defer audioStream.Close()

			mp3Path := filepath.Join("./download", bvid, fmt.Sprintf("%s.mp3", info.PartName))
			if err := convertToMp3Stream(audioStream, mp3Path); err != nil {
				ch <- fmt.Errorf("转换音频失败: %w", err)
				return
			}
			fmt.Printf("下载%s完成\n", info.PartName)
		}(info)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	// 处理错误信息
	for err := range ch {
		if err != nil {
			fmt.Fprintf(os.Stderr, "下载发生错误: %v\n", err)
		}
	}
}
