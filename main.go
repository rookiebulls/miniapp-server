package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v2"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const host = "http://127.0.0.1"

var (
	port       string
	currentDir string
	mu         sync.Mutex
	conf       config
)

var logger = log.New(os.Stdout, "[MINIAPP-SERVER] - ", log.Ldate|log.Ltime|log.Lshortfile)
var clients = make(map[*websocket.Conn]bool)
var lines = make(chan string)
var upgrader = websocket.Upgrader{} // use default options

type payload struct {
	Project  string `json:"project"`
	Branch   string `json:"branch"`
	BuildEnv string `json:"buildEnv"`
	Version  string `json:"version"`
	Desc     string `json:"desc"`
}

type msgEvt struct {
	Event string  `json:"event"`
	Data  payload `json:"data"`
}

type pic struct {
	Base64 string `json:"base64"`
	Md5    string `json:"md5"`
}

type imgMsg struct {
	Msgtype string `json:"msgtype"`
	Image   pic    `json:"image"`
}

type text struct {
	Content             string   `json:"content"`
	MentionedMobileList []string `json:"mentioned_mobile_list"`
}

type textMsg struct {
	Msgtype string `json:"msgtype"`
	Text    text   `json:"text"`
}

type wsResp struct {
	Line string `json:"line"`
}

type git struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Remote   string `yaml:"remote"`
	Bin      string `yaml:"bin"`
}

type config struct {
	IdePath       string   `yaml:"ide_path"`
	CliPath       string   `yaml:"cli_path"`
	Git           git      `yaml:"git"`
	Webhook       string   `yaml:"webhook"`
	NotifiedUsers []string `yaml:"notified_users"`
}

func sendToWechat(toJson []byte) error {
	resp, err := http.Post(conf.Webhook, "application/json", bytes.NewReader(toJson))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	logger.Println(string(body))
	return nil
}

func sendImgMsgToWechat(b64str string, md5str string) error {
	var im = pic{b64str, md5str}
	var weMsg = imgMsg{"image", im}
	toJson, _ := json.Marshal(weMsg)
	return sendToWechat(toJson)
}

func sendTextMsgToWechat(p payload) error {
	desc := p.Desc
	if desc == "" {
		desc = "修复一些bug"
	}
	mentionedList := conf.NotifiedUsers
	var txt = text{desc, mentionedList}
	var weMsg = textMsg{"text", txt}
	toJson, _ := json.Marshal(weMsg)
	return sendToWechat(toJson)
}

func getListeningPort() string {
	data, err := ioutil.ReadFile(conf.IdePath)
	if err != nil {
		logger.Panic(err)
	}
	return string(data)
}

func init() {
	currentDir, _ = os.Getwd()
	configFile := filepath.Join(currentDir, "config.yml")
	conf = config{}
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		logger.Panic(err)
	}
	err = yaml.Unmarshal(data, &conf)
	if err != nil {
		logger.Panic(err)
	}
	port = getListeningPort()
	logger.Println("IDE监听端口", port)
}

func runCommand(l chan string, name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	// 命令的错误输出和标准输出都连接到同一个管道
	stdout, err := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout

	if err != nil {
		return err
	}

	if err = cmd.Start(); err != nil {
		return err
	}
	// 从管道中实时获取输出
	output := bufio.NewScanner(stdout)
	for output.Scan() {
		l <- output.Text()
	}

	if err = cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func genURL(path string, p payload) string {
	project := p.Project
	distpath := filepath.Join(currentDir, project, "dist")
	targetURL := fmt.Sprintf("%s:%s/%s", host, port, path)
	encodedURL, _ := url.Parse(targetURL)
	params := url.Values{}
	params.Add("format", "base64")
	params.Add("projectpath", distpath)
	if p.Version != "" {
		params.Add("version", p.Version)
	}
	if p.Desc != "" {
		params.Add("desc", p.Desc)
	}
	encodedURL.RawQuery = params.Encode()
	return encodedURL.String()
}

func launch(l chan string) error {
	err := runCommand(l, conf.CliPath, "-o")
	if err != nil {
		return err
	}
	port = getListeningPort()
	logger.Println("IDE监听端口", port)
	return nil
}

func login(l chan string) error {
	targetURL := fmt.Sprintf("%s:%s/login?format=base64", host, port)
	logger.Println(targetURL)
	resp, err := http.Get(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	l <- "微信扫码登录"
	l <- string(body)
	return nil
}

func preview(l chan string, p payload) error {
	targetURL := genURL("preview", p)
	logger.Println(targetURL)
	resp, err := http.Get(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	bodyStr := string(body)
	if strings.Contains(bodyStr, "code") && strings.Contains(bodyStr, "error") {
		l <- bodyStr
	} else {
		l <- "微信扫码预览"
		b64 := "data:image/jpeg;base64," + bodyStr
		l <- b64
	}
	return nil
}

func publish(l chan string, p payload) error {
	targetURL := genURL("upload", p)
	logger.Println(targetURL)
	resp, err := http.Get(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	bodyStr := string(body)
	if strings.Contains(bodyStr, "code") && strings.Contains(bodyStr, "error") {
		l <- bodyStr
	} else {
		l <- "发布成功"
		qrcode := filepath.Join(currentDir, "assets", p.Project+".png")
		img, err := ioutil.ReadFile(qrcode)
		if err != nil {
			logger.Println("打开二维码图片失败")
			return err
		}
		imgb64 := base64.StdEncoding.EncodeToString(img)
		b64 := "data:image/jpeg;base64," + imgb64
		l <- b64
		h := md5.New()
		h.Write(img)
		imgmd5 := hex.EncodeToString(h.Sum(nil))
		err = sendImgMsgToWechat(imgb64, imgmd5)
		if err != nil {
			logger.Println("发送图片到企业微信失败")
			return err
		}
		err = sendTextMsgToWechat(p)
		if err != nil {
			logger.Println("发送文字到企业微信失败")
			return err
		}
	}
	return nil
}

func quit(l chan string) error {
	targetURL := fmt.Sprintf("%s:%s/quit", host, port)
	logger.Println(targetURL)
	resp, err := http.Get(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	l <- "退出成功"
	return nil
}

func clone(l chan string, p payload) error {
	project := p.Project

	repo := fmt.Sprintf("https://%s:%s@%s/%s.git", conf.Git.Username, conf.Git.Password, conf.Git.Remote, project)
	logger.Printf("克隆远程仓库: %s", repo)
	projectpath := filepath.Join(currentDir, project)
	err := runCommand(l, conf.Git.Bin, "clone", repo, projectpath)
	if err != nil {
		return err
	}
	return nil
}

func fetch(l chan string, p payload) error {
	branch := p.Branch

	logger.Printf("fetch远程分支: %s", branch)
	err := runCommand(l, conf.Git.Bin, "fetch", "origin", branch)
	if err != nil {
		return err
	}
	return nil
}

func checkout(l chan string, p payload) error {
	branch := p.Branch

	logger.Printf("checkout分支: %s", branch)
	err := runCommand(l, conf.Git.Bin, "checkout", branch)
	if err != nil {
		return err
	}
	return nil
}

func pull(l chan string, p payload) error {
	branch := p.Branch

	logger.Printf("更新远程分支: %s", branch)
	err := runCommand(l, conf.Git.Bin, "pull", "origin", branch)
	if err != nil {
		return err
	}
	return nil
}

func beforeInstall(l chan string, p payload) error {
	project := p.Project
	projectpath := filepath.Join(currentDir, project)
	_, err := os.Stat(projectpath)
	if os.IsNotExist(err) {
		err = clone(l, p)
		if err != nil {
			return err
		}
	}
	err = os.Chdir(projectpath)
	if err != nil {
		return err
	}
	err = fetch(l, p)
	if err != nil {
		return err
	}
	err = checkout(l, p)
	if err != nil {
		return err
	}
	err = pull(l, p)
	if err != nil {
		return err
	}
	return nil
}

func install(l chan string, p payload) error {
	logger.Println("安装依赖...")
	err := runCommand(l, "npm", "install")
	if err != nil {
		return err
	}
	return nil
}

func build(l chan string, p payload) error {
	env := p.BuildEnv

	logger.Println("编译版本...")
	err := runCommand(l, "npm", "run", "build:"+env)
	if err != nil {
		return err
	}
	return nil
}

func onLogin(l chan string) error {
	err := launch(l)
	if err != nil {
		return err
	}
	err = login(l)
	if err != nil {
		return err
	}
	return nil
}

func onPreview(l chan string, p payload) error {
	err := beforeInstall(lines, p)
	if err != nil {
		return err
	}
	err = install(lines, p)
	if err != nil {
		return err
	}
	err = build(lines, p)
	if err != nil {
		return err
	}
	err = preview(lines, p)
	if err != nil {
		return err
	}
	return nil
}

func onPublish(l chan string, p payload) error {
	err := beforeInstall(lines, p)
	if err != nil {
		return err
	}
	err = install(lines, p)
	if err != nil {
		return err
	}
	err = build(lines, p)
	if err != nil {
		return err
	}
	err = publish(lines, p)
	if err != nil {
		return err
	}
	return nil
}

func handleMessages() {
	for {
		select {
		case line := <-lines:
			resp := wsResp{line}
			for c := range clients {
				err := c.WriteJSON(resp)
				if err != nil {
					delete(clients, c)
				}
			}
		}
	}
}

func echoHandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	clients[c] = true
	for {
		var m msgEvt
		err := c.ReadJSON(&m)
		if err != nil {
			logger.Println("read:", err)
			delete(clients, c)
			break
		}
		mu.Lock()
		logger.Println("locking...")
		evt, _ := json.Marshal(m)
		evtStr := string(evt)
		remoteAddr := fmt.Sprint(c.RemoteAddr())
		lines <- fmt.Sprintf("%s - SEND: %s", remoteAddr, evtStr)
		logger.Printf("recv: %s from address %s", evtStr, remoteAddr)
		switch m.Event {
		case "quit":
			err = quit(lines)
		case "login":
			err = onLogin(lines)
		case "preview":
			err = onPreview(lines, m.Data)
		case "publish":
			err = onPublish(lines, m.Data)
		default:
			err = errors.New("无效消息")
		}

		if err != nil {
			logger.Printf("Error: %v", err.Error())
			lines <- err.Error()
		}
		logger.Println("unlocking...")
		lines <- "DONE"
		mu.Unlock()

	}
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	indexHTML := filepath.Join(currentDir, "assets", "index.html")
	tpl, err := template.ParseFiles(indexHTML)
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}
	tpl.Execute(w, "ws://"+r.Host+"/echo")
}

func main() {
	port := "8080"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/echo", echoHandler)

	go handleMessages()
	logger.Println("服务已启动...", port)
	logger.Fatal(http.ListenAndServe(":"+port, nil))
}
