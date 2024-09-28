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
	Cookie        string `json:"cookie"`
	DownloadVideo bool   `json:"downloadVideo"`
	VideoEncoder  string `json:"videoEncoder"`
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
		"referer":            "https://www.bilibili.com/video",
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

// getStreamUrl 获取流地址
func getStreamUrl(bvid string, cid string, headers map[string]string) (string, string, error) {
	url := "https://api.bilibili.com/x/player/wbi/playurl"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", "", err
	}

	q := req.URL.Query()
	q.Add("fnval", "4048")
	q.Add("bvid", bvid)
	q.Add("cid", cid)
	q.Add("qn", "116")
	req.URL.RawQuery = q.Encode()

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Dash struct {
				Video []struct {
					BaseUrl string `json:"baseUrl"`
					Id      int    `json:"id"`
					Width   int    `json:"width"`
					Height  int    `json:"Height"`
				} `json:"video"`
				Audio []struct {
					BaseUrl string `json:"baseUrl"`
					Id      int    `json:"id"`
				} `json:"audio"`
				Flac struct {
					Audio struct {
						BaseUrl string `json:"baseUrl"`
						Id      int    `json:"id"`
					} `json:"audio"`
				} `json:"flac"`
			} `json:"dash"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}

	var AudioUrl = result.Data.Dash.Audio[0].BaseUrl
	var VideoUrl = result.Data.Dash.Video[0].BaseUrl
	// 优先返回hi-res的数据
	if result.Data.Dash.Flac.Audio.BaseUrl != "" {
		AudioUrl = result.Data.Dash.Flac.Audio.BaseUrl
	}
	return AudioUrl, VideoUrl, nil
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

	cmd := exec.Command("ffmpeg", "-i", "pipe:0", "-q:a", "0", "-f", "mp3", outputPath)
	cmd.Stdin = inputStream
	return cmd.Run()
}

func convertToVideoStream(videoStream io.Reader, audioPath string, outputPath string, config MiaoConfig) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	// fmt.Print(audioPath)

	cmd := exec.Command("ffmpeg",
		"-i", "pipe:0",
		"-i", audioPath,
		"-c:v", config.VideoEncoder,
		"-f", "mp4", outputPath)
	cmd.Stdin = videoStream
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		fmt.Println("Error running ffmpeg:", err)
		return err
	}

	return nil
}

func logConfig(config *MiaoConfig) {
	mode := "audio"
	if config.DownloadVideo {
		mode = fmt.Sprintf("audio+video \nvideo encoder: %s", config.VideoEncoder)
	}
	fmt.Printf("mode: %s \n", mode)
}

func main() {
	// 读取配置文件
	config, err := readConfig("./miaoConfig.json")
	if err != nil {
		panic(err)
	}

	logConfig(config)

	// 获取请求头
	headers := getHeaders(config)

	// 从控制台读取bvid
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("bvid: ")
	bvid, _ := reader.ReadString('\n')
	bvid = strings.TrimSpace(bvid)

	infoList, err := getVideoInfo(bvid, headers)
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	ch := make(chan error, len(infoList)) // 用于传递错误信息的channel
	const maxAsync = 999
	var asyncCount = 0
	var downloadMp3 = func(info VideoInfo, index int) {
		const maxRetries = 8
		var err error
		var audioUrl string
		var videoUrl string
		var audioStream io.ReadCloser
		var videoStream io.ReadCloser

		for attempt := 1; attempt <= maxRetries; attempt++ {
			audioUrl, videoUrl, err = getStreamUrl(bvid, info.Cid, headers)
			if err != nil {
				fmt.Printf("获取音频地址失败 (尝试 %d/%d): %v\n", attempt, maxRetries, err)
				continue
			}

			audioStream, err = getFileStream(audioUrl, headers)
			if err != nil {
				fmt.Printf("获取音频流失败 (尝试 %d/%d): %v\n", attempt, maxRetries, err)
				continue
			}
			defer audioStream.Close()

			audioPath := filepath.Join("./download", fmt.Sprintf("%s-%d-%s.mp3", bvid, index, info.PartName))
			if err = convertToMp3Stream(audioStream, audioPath); err != nil {
				fmt.Printf("转换音频失败 (尝试 %d/%d): %v\n", attempt, maxRetries, err)
				continue
			}

			if config.DownloadVideo {
				videoStream, err = getFileStream(videoUrl, headers)
				if err != nil {
					fmt.Printf("获取视频流失败 (尝试 %d/%d): %v\n", attempt, maxRetries, err)
					continue
				}
				defer videoStream.Close()

				videoPath := filepath.Join("./download", fmt.Sprintf("%s-%d-%s.mp4", bvid, index, info.PartName))
				if err = convertToVideoStream(videoStream, audioPath, videoPath, *config); err != nil {
					fmt.Printf("转换视频失败 (尝试 %d/%d): %v\n", attempt, maxRetries, err)
					continue
				}
			}

			fmt.Printf("下载%s完成\n", info.PartName)
			wg.Done()
			return
		}
		wg.Done()
		ch <- fmt.Errorf("下载%s失败: %w", info.PartName, err)
	}

	for index, info := range infoList {
		wg.Add(1)
		asyncCount += 1
		go downloadMp3(info, index+1)
		if asyncCount == maxAsync {
			wg.Wait()
			asyncCount = 0
		}
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
