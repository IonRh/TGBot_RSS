package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/mattn/go-sqlite3"
)

// 版本信息变量，可以通过 -ldflags 在构建时设置
var (
	version   = "v1.0.0"     // 版本号
	buildTime = "2025-05-07" // 构建时间
	gitCommit = "unknown"    // Git提交哈希(可选)
)

// Config 应用配置结构体
// 从config.json文件中加载配置信息
type Config struct {
	BotToken  string `json:"BotToken"`  // Telegram Bot API令牌
	ADMINIDS  int64  `json:"ADMINIDS"`  // 管理员ID，逗号分隔
	Cycletime int    `json:"Cycletime"` // RSS检查周期(秒)
	Debug     bool   `json:"Debug"`     // 是否开启调试模式
	ProxyURL  string `json:"ProxyURL"`  // 代理服务器URL
	Pushinfo  string `json:"Pushinfo"`  // 推送信息配置
}

// Message RSS消息结构体
// 用于存储解析后的RSS条目信息
type Message struct {
	Title       string    // 消息标题
	Description string    // 消息描述/内容
	Link        string    // 原文链接
	PubDate     time.Time // 发布时间
}

// Subscription RSS订阅结构体
type Subscription struct {
	ID      int     // 数据库中的唯一ID
	URL     string  // RSS源URL
	Name    string  // 订阅名称
	Users   []int64 // 订阅用户ID列表
	Channel int     // 是否推送给所有用户
}

// UserState 用户状态结构体
// 用于跟踪用户当前的交互状态
type UserState struct {
	Action    string                 // 当前操作，如"add_keyword", "add_subscription"
	MessageID int                    // 相关消息ID
	Data      map[string]interface{} // 状态相关的附加数据
}

// 全局变量
var (
	globalConfig *Config                      // 全局配置对象
	db           *sql.DB                      // 数据库连接
	bot          *tgbotapi.BotAPI             // Telegram Bot API客户端
	userStates   = make(map[int64]*UserState) // 用户状态映射表
	stateMutex   sync.RWMutex                 // 用户状态读写锁
	dbMutex      sync.RWMutex                 // 数据库操作读写锁
)

// 数据结构
type UserStats struct {
	SubscriptionCount int
	KeywordCount      int
}

type SubscriptionInfo struct {
	Name       string
	URL        string
	LastUpdate string
}

var cyclenum int

// 常量定义
const (
	MaxMessageLength = 4000             // Telegram消息最大长度
	DatabaseTimeout  = 30 * time.Second // 数据库操作超时时间
	HTTPTimeout      = 60 * time.Second // HTTP请求超时时间
	LogFile          = "bot.log"        // 日志文件路径
	DBFile           = "tgbot.db"       // 数据库文件路径
	ConfigFile       = "config.json"    // 配置文件路径
	DefaultCycleTime = 300              // 默认RSS检查周期(秒)
)

// BotError 自定义错误类型
// 用于包装错误信息，便于日志记录和错误处理

// 推送统计结构体
type PushStats struct {
	Date      string         // 日期，格式为 YYYY-MM-DD
	TotalPush int            // 总推送次数
	ByRSS     map[string]int // 每个RSS源的推送次数
	mutex     sync.Mutex     // 互斥锁，保护统计数据
}

// 全局变量，存储当日推送统计
var DailyPushStats = &PushStats{
	Date:  time.Now().Format("2006-01-02"),
	ByRSS: make(map[string]int),
}

type DatabaseOperator struct {
	db *sql.DB
}

// 重置推送统计
func resetPushStatsIfNeeded() {
	DailyPushStats.mutex.Lock()
	defer DailyPushStats.mutex.Unlock()

	currentDate := time.Now().Format("2006-01-02")
	if DailyPushStats.Date != currentDate {
		// 日期变更，打印昨日统计并重置
		if DailyPushStats.TotalPush > 0 {
			logMessage("info", fmt.Sprintf("日期变更，%s推送统计：总计 %d 次，统计清零。",
				DailyPushStats.Date, DailyPushStats.TotalPush))
		}

		DailyPushStats.Date = currentDate
		DailyPushStats.TotalPush = 0
		DailyPushStats.ByRSS = make(map[string]int)
	}
}

// 记录推送
func recordPush(rssName string) {
	DailyPushStats.mutex.Lock()
	defer DailyPushStats.mutex.Unlock()

	// 检查日期，如果日期变更则重置统计
	currentDate := time.Now().Format("2006-01-02")
	if DailyPushStats.Date != currentDate {
		// 日期已变更，这里不打印，避免重复打印
		DailyPushStats.Date = currentDate
		DailyPushStats.TotalPush = 0
		DailyPushStats.ByRSS = make(map[string]int)
	}

	// 更新统计
	DailyPushStats.TotalPush++
	DailyPushStats.ByRSS[rssName]++
}

// 获取推送统计信息
func GetPushStatsInfo() string {
	DailyPushStats.mutex.Lock()
	defer DailyPushStats.mutex.Unlock()

	// 构建统计信息
	info := fmt.Sprintf("📊 今日(%s)推送总计：%d 次",
		DailyPushStats.Date, DailyPushStats.TotalPush)

	// 按RSS源统计
	if len(DailyPushStats.ByRSS) > 0 {
		info += "\n"
		for rssName, count := range DailyPushStats.ByRSS {
			info += fmt.Sprintf("📊 %s: %d 次\n", rssName, count)
		}
	}

	return info
}

// loadConfig 从配置文件加载配置
// 返回配置对象和可能的错误
func loadConfig() (*Config, error) {
	file, err := os.Open(ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("打开配置文件失败: %v", err)
	}
	defer file.Close()

	var config Config
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 验证必要配置
	if config.BotToken == "" {
		return nil, fmt.Errorf("BotToken不能为空")
	}

	// 设置默认值
	if config.Cycletime <= 0 {
		config.Cycletime = DefaultCycleTime
	}

	return &config, nil
}

// logMessage 记录日志消息
// 支持不同日志级别和用户ID标记
func logMessage(level, message string, userID ...int64) {
	// 日志级别颜色映射
	colors := map[string]string{
		"info":  "\033[32m", // 绿色
		"error": "\033[31m", // 红色
		"debug": "\033[34m", // 蓝色
		"warn":  "\033[33m", // 黄色
	}

	// 日志级别图标映射
	icons := map[string]string{
		"info":  "ℹ️",
		"error": "❌",
		"debug": "🐞",
		"warn":  "⚠️",
	}

	// 调试日志级别检查
	if level == "debug" && (globalConfig == nil || !globalConfig.Debug) {
		return
	}

	color := colors[level]
	icon := icons[level]
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	userInfo := ""
	if len(userID) > 0 && userID[0] != 0 {
		userInfo = fmt.Sprintf(" [User:%d]", userID[0])
	}

	// 格式化日志文本
	logText := fmt.Sprintf("%s [%s]%s %s%s", timestamp, level, userInfo, icon, message)

	// 控制台输出（带颜色）
	fmt.Printf("\033[36m%s\033[0m %s%s\033[0m %s%s\033[0m%s\n",
		timestamp, color, strings.ToUpper(level), icon, message, userInfo)

	// 写入日志文件（无颜色）
	writeToLogFile(logText)
}

// writeToLogFile 将日志写入文件
func writeToLogFile(message string) {
	file, err := os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// 如果无法打开日志文件，输出到标准错误
		fmt.Fprintf(os.Stderr, "无法打开日志文件: %v\n", err)
		return
	}
	defer file.Close()

	// 添加换行符写入文件
	if _, err := file.WriteString(message + "\n"); err != nil {
		fmt.Fprintf(os.Stderr, "写入日志失败: %v\n", err)
	}
}

func createHTTPClient(proxyURL string) *http.Client {
	// 默认传输配置
	transport := &http.Transport{
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
	}

	// 基本客户端配置
	client := &http.Client{
		Timeout:   HTTPTimeout,
		Transport: transport,
	}

	// 如果提供了代理URL，配置代理
	if proxyURL != "" {
		if proxyURLParsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyURLParsed)
			if cyclenum == 0 {
				logMessage("info", "使用代理: "+proxyURL)
			}
		} else {
			logMessage("error", "代理URL解析失败: "+err.Error())
		}
	}

	return client
}

// 用户状态管理函数

// setUserState 设置用户状态
// 用于跟踪用户当前的交互状态和上下文
func setUserState(userID int64, action string, messageID int, data map[string]interface{}) {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	if data == nil {
		data = make(map[string]interface{})
	}

	userStates[userID] = &UserState{
		Action:    action,
		MessageID: messageID,
		Data:      data,
	}

	logMessage("debug", fmt.Sprintf("用户状态已设置: %s", action), userID)
}

// getUserState 获取用户状态
// 返回用户当前的状态对象，如果不存在则返回nil
func getUserState(userID int64) *UserState {
	stateMutex.RLock()
	defer stateMutex.RUnlock()
	return userStates[userID]
}

// clearUserState 清除用户状态
// 在操作完成或取消时调用
func clearUserState(userID int64) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	delete(userStates, userID)
	logMessage("debug", "用户状态已清除", userID)
}

// withDB 数据库操作包装器
// 提供数据库连接和事务管理
func withDB(operation func(*sql.DB) error) error {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), DatabaseTimeout)
	defer cancel()

	// 检查数据库连接
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("数据库连接失败: %v", err)
	}

	// 执行数据库操作
	return operation(db)
}

// 统一消息发送接口
type MessageSender struct {
	bot *tgbotapi.BotAPI
}

func NewMessageSender(bot *tgbotapi.BotAPI) *MessageSender {
	return &MessageSender{bot: bot}
}

// SendResponse 统一的消息发送方法
func (m *MessageSender) SendResponse(userID int64, messageID int, text string, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	if messageID > 0 {
		// 编辑现有消息
		edit := tgbotapi.NewEditMessageText(userID, messageID, text)
		if keyboard != nil {
			edit.ReplyMarkup = keyboard
		}
		_, err := m.bot.Send(edit)
		return err
	} else {
		// 发送新消息
		msg := tgbotapi.NewMessage(userID, text)
		if keyboard != nil {
			msg.ReplyMarkup = *keyboard
		}
		_, err := m.bot.Send(msg)
		return err
	}
}

// SendHTMLResponse 发送HTML格式的消息
func (m *MessageSender) SendHTMLResponse(userID int64, messageID int, text string, keyboard *tgbotapi.InlineKeyboardMarkup, disablePreview ...bool) error {
	logMessage("debug", fmt.Sprintf("SendHTMLResponse: messageID=%d, textLen=%d", messageID, len(text)), userID)

	// 处理可选的 disablePreview 参数，默认为 false（显示预览）
	shouldDisablePreview := false
	if len(disablePreview) > 0 {
		shouldDisablePreview = disablePreview[0]
	}

	if messageID > 0 {
		// 编辑现有消息
		edit := tgbotapi.NewEditMessageText(userID, messageID, text)
		edit.ParseMode = "HTML"
		edit.DisableWebPagePreview = shouldDisablePreview
		if keyboard != nil {
			edit.ReplyMarkup = keyboard
		}
		logMessage("debug", "准备编辑消息", userID)
		_, err := m.bot.Send(edit)
		if err != nil {
			logMessage("error", fmt.Sprintf("编辑消息失败: %v", err), userID)
		}
		return err
	} else {
		// 发送新消息
		msg := tgbotapi.NewMessage(userID, text)
		msg.ParseMode = "HTML"
		msg.DisableWebPagePreview = shouldDisablePreview
		if keyboard != nil {
			msg.ReplyMarkup = *keyboard
		}
		logMessage("debug", "准备发送新消息", userID)
		_, err := m.bot.Send(msg)
		if err != nil {
			logMessage("error", fmt.Sprintf("发送新消息失败: %v", err), userID)
		}
		return err
	}
}

// SendError 发送错误消息
func (m *MessageSender) SendError(userID int64, messageID int, errorText string) {
	keyboard := CreateBackButton()
	if err := m.SendResponse(userID, messageID, errorText, &keyboard); err != nil {
		logMessage("error", fmt.Sprintf("发送错误消息失败: %v", err), userID)
	}
}

// HandleLongText 处理长文本消息
func (m *MessageSender) HandleLongText(userID int64, messageID int, text string, addBackButton bool) {
	if len(text) <= MaxMessageLength {
		var keyboard *tgbotapi.InlineKeyboardMarkup
		if addBackButton {
			kb := CreateBackButton()
			keyboard = &kb
		}
		m.SendResponse(userID, messageID, text, keyboard)
		return
	}

	// 删除原消息并分段发送
	if messageID > 0 {
		deleteMsg := tgbotapi.NewDeleteMessage(userID, messageID)
		m.bot.Request(deleteMsg)
	}

	chunks := splitMessage(text, MaxMessageLength)
	for i, chunk := range chunks {
		var keyboard *tgbotapi.InlineKeyboardMarkup
		if addBackButton && i == len(chunks)-1 {
			kb := CreateBackButton()
			keyboard = &kb
		}
		m.SendResponse(userID, 0, chunk, keyboard)
		if i < len(chunks)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// 统一键盘创建函数
func CreateBackButton() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 返回主菜单", "back_to_menu"),
		),
	)
}

func CreateDeleteKeyboard(items []string, prefix string) tgbotapi.InlineKeyboardMarkup {
	const buttonsPerRow = 3
	var keyboardRows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton

	for i, item := range items {
		currentRow = append(currentRow, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("❌ %s", item),
			fmt.Sprintf("%s_%s", prefix, item),
		))

		if len(currentRow) == buttonsPerRow || i == len(items)-1 {
			keyboardRows = append(keyboardRows, currentRow)
			currentRow = []tgbotapi.InlineKeyboardButton{}
		}
	}

	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{})
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 返回主菜单", "back_to_menu"),
	))

	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboardRows}
}

// 统一的数据库操作接口

func NewDatabaseOperator(db *sql.DB) *DatabaseOperator {
	return &DatabaseOperator{db: db}
}

func (d *DatabaseOperator) ExecuteWithTransaction(operation func(*sql.Tx) error) error {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), DatabaseTimeout)
	defer cancel()

	if err := d.db.PingContext(ctx); err != nil {
		return fmt.Errorf("数据库连接失败: %v", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := operation(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DatabaseOperator) Execute(operation func(*sql.DB) error) error {
	return withDB(operation)
}

// 统一的用户操作处理器
type UserActionHandler struct {
	sender *MessageSender
	dbOp   *DatabaseOperator
}

func NewUserActionHandler(sender *MessageSender, dbOp *DatabaseOperator) *UserActionHandler {
	return &UserActionHandler{
		sender: sender,
		dbOp:   dbOp,
	}
}

// HandleAction 统一的操作处理方法
func (h *UserActionHandler) HandleAction(userID int64, messageID int, actionType, action string, data ...string) {
	switch actionType {
	case "keyword":
		h.handleKeywordAction(userID, messageID, action, data...)
	case "subscription":
		h.handleSubscriptionAction(userID, messageID, action, data...)
	}
}

func (h *UserActionHandler) handleKeywordAction(userID int64, messageID int, action string, data ...string) {
	switch action {
	case "add_prompt":
		setUserState(userID, "add_keyword", messageID, nil)
		text := "请输入要添加的关键词，多个关键词可用逗号分隔：\n\n💡 技巧：可使用(*)或者(-)进行过滤匹配\n * 可匹配任意字符，-关键词 表示屏蔽\n示例：你*帅*   可匹配 你好帅呀！\n示例：-不喜欢  可屏蔽包含 不喜欢 的内容\n\n💡 匹配范围：可使用前缀指定匹配范围\n#t 关键词 - 只匹配标题\n#c 关键词 - 只匹配描述内容\n#a 关键词 - 匹配标题和描述\n示例：#t技术  只在标题中匹配\"技术\"\n示例：#c新闻  只在描述中匹配\"新闻\"\n示例：#a科技  在标题和描述中都匹配\"科技\"\n\n💡 RSS过滤：可使用(+)指定RSS源\n示例：技术+科技新闻  只匹配名为\"科技新闻\"的RSS源\n示例：技术  匹配所有RSS源\n\n💡 提示：全推送可用*号"
		keyboard := CreateBackButton()
		h.sender.SendResponse(userID, messageID, text, &keyboard)

	case "add":
		if len(data) == 0 {
			h.sender.SendError(userID, messageID, "❌ 请输入有效的关键词")
			return
		}

		result, err := h.addKeywords(userID, data)
		if err != nil {
			logMessage("error", fmt.Sprintf("添加关键词失败: %v", err), userID)
			h.sender.SendError(userID, messageID, "添加关键词失败，请稍后重试")
			return
		}
		clearUserState(userID)
		keyboard := CreateBackButton()
		h.sender.SendResponse(userID, messageID, result, &keyboard)

	case "view":
		h.viewKeywords(userID, messageID)

	case "delete_list":
		h.showDeleteKeywords(userID, messageID)

	case "delete":
		if len(data) == 0 {
			h.sender.SendError(userID, messageID, "删除关键词失败：参数错误")
			return
		}
		h.deleteKeyword(userID, messageID, data[0])
	}
}

func (h *UserActionHandler) handleSubscriptionAction(userID int64, messageID int, action string, data ...string) {
	switch action {
	case "add_prompt":
		setUserState(userID, "add_subscription", messageID, nil)
		text := `✏️ 手动添加新订阅：
⚠️ 频道需要先转为rss才可添加
请按以下格式输入RSS订阅信息：

URL 名称 TG频道用1常规用0

📝 示例：
常规订阅：https://example.com/feed 科技新闻 0
频道订阅：https://example.com/channel/feed TG资讯播报 1`
		keyboard := CreateBackButton()
		h.sender.SendResponse(userID, messageID, text, &keyboard)

	case "add":
		//fmt.Println(data[0])
		//fmt.Println(data[1])
		if len(data) < 3 {
			h.sender.SendError(userID, messageID, "❌ 格式错误！请按照以下格式输入：\nURL 名称 TG频道用1常规用0\n例如：https://example.com/feed 科技新闻 0")
			return
		}

		h.addSubscription(userID, messageID, data[0], data[1], data[2])

	case "view":
		h.viewSubscriptions(userID, messageID)

	case "delete_list":
		h.showDeleteSubscriptions(userID, messageID)

	case "delete":
		if len(data) == 0 {
			h.sender.SendError(userID, messageID, "删除订阅失败：参数错误")
			return
		}
		h.deleteSubscription(userID, messageID, data[0])
	}
}

// 关键词相关方法
func (h *UserActionHandler) addKeywords(userID int64, keywords []string) (string, error) {
	// 验证关键词长度
	//for _, kw := range keywords {
	//	if len(kw) > 50 {
	//		return "", fmt.Errorf("关键词长度不能超过50个字符")
	//	}
	//}
	return addKeywordsForUser(userID, keywords)
}

func (h *UserActionHandler) viewKeywords(userID int64, messageID int) {
	keywords, err := getKeywordsForUser(userID)
	if err != nil {
		logMessage("error", fmt.Sprintf("获取用户关键词失败: %v", err), userID)
		h.sender.SendError(userID, messageID, "获取关键词失败，请稍后重试")
		return
	}

	if len(keywords) == 0 {
		h.sender.SendError(userID, messageID, "你还没有添加任何关键词\n\n点击 📝 添加关键词 开始使用")
		return
	}

	sort.Strings(keywords)
	text := h.formatKeywordsList(keywords)
	keyboard := CreateBackButton()
	h.sender.SendHTMLResponse(userID, messageID, text, &keyboard)
}

func (h *UserActionHandler) showDeleteKeywords(userID int64, messageID int) {
	keywords, err := getKeywordsForUser(userID)
	if err != nil {
		logMessage("error", fmt.Sprintf("获取用户关键词失败: %v", err), userID)
		h.sender.SendError(userID, messageID, "获取关键词失败，请稍后重试")
		return
	}

	if len(keywords) == 0 {
		h.sender.SendError(userID, messageID, "你还没有添加任何关键词")
		return
	}

	sort.Strings(keywords)
	keyboard := CreateDeleteKeyboard(keywords, "del_kw")
	h.sender.SendResponse(userID, messageID, "请选择要删除的关键词：", &keyboard)
}

func (h *UserActionHandler) deleteKeyword(userID int64, messageID int, keyword string) {
	result, err := removeKeywordForUser(userID, keyword)
	if err != nil {
		logMessage("error", fmt.Sprintf("删除关键词失败: %v", err), userID)
		h.sender.SendError(userID, messageID, "删除关键词失败，请稍后重试")
		return
	}

	keyboard := CreateBackButton()
	h.sender.SendResponse(userID, messageID, result, &keyboard)

	// 如果还有关键词，1秒后刷新删除选项
	go func() {
		time.Sleep(time.Second)
		keywords, err := getKeywordsForUser(userID)
		if err == nil && len(keywords) > 0 {
			h.showDeleteKeywords(userID, messageID)
		}
	}()
}

// 订阅相关方法
func (h *UserActionHandler) addSubscription(userID int64, messageID int, feedURL, name, channel string) {
	feedURL = strings.TrimSpace(feedURL)
	name = strings.TrimSpace(name)

	//if len(name) > 100 {
	//	h.sender.SendError(userID, messageID, "❌ 订阅名称长度不能超过100个字符")
	//	return
	//}

	if err := validateAndProcessSubscription(feedURL, name, channel, userID); err != nil {
		logMessage("error", fmt.Sprintf("添加订阅失败: %v", err), userID)
		h.sender.SendError(userID, messageID, "❌ "+err.Error())
		return
	}

	clearUserState(userID)
	keyboard := CreateBackButton()
	text := fmt.Sprintf("✅ 成功添加订阅：\n📰 %s\n🔗 %s", name, feedURL)
	logMessage("info", fmt.Sprintf("✅ 成功添加订阅：📰 %s  🔗 %s", name, feedURL))
	h.sender.SendResponse(userID, messageID, text, &keyboard)
}

func (h *UserActionHandler) viewSubscriptions(userID int64, messageID int) {
	subscriptions, err := getSubscriptionsForUser(userID)
	if err != nil {
		logMessage("error", fmt.Sprintf("获取用户订阅失败: %v", err), userID)
		h.sender.SendError(userID, messageID, "获取订阅失败，请稍后重试")
		return
	}

	if len(subscriptions) == 0 {
		h.sender.SendError(userID, messageID, "你还没有添加任何订阅\n\n点击 ➕ 添加订阅 开始使用")
		return
	}

	text := h.formatSubscriptionsList(subscriptions)
	keyboard := CreateBackButton()
	h.sender.SendHTMLResponse(userID, messageID, text, &keyboard)
}

func (h *UserActionHandler) showDeleteSubscriptions(userID int64, messageID int) {
	subscriptions, err := getSubscriptionsForUser(userID)
	if err != nil {
		logMessage("error", fmt.Sprintf("获取用户订阅失败: %v", err), userID)
		h.sender.SendError(userID, messageID, "获取订阅失败，请稍后重试")
		return
	}

	if len(subscriptions) == 0 {
		h.sender.SendError(userID, messageID, "你还没有添加任何订阅")
		return
	}

	var names []string
	for _, sub := range subscriptions {
		names = append(names, sub.Name)
	}

	keyboard := CreateDeleteKeyboard(names, "del_sub")
	h.sender.SendResponse(userID, messageID, "请选择要删除的订阅：", &keyboard)
}

func (h *UserActionHandler) deleteSubscription(userID int64, messageID int, subscriptionName string) {
	result, err := removeSubscriptionForUser(userID, subscriptionName)
	if err != nil {
		logMessage("error", fmt.Sprintf("删除订阅失败: %v", err), userID)
		h.sender.SendError(userID, messageID, "删除订阅失败，请稍后重试")
		return
	}

	keyboard := CreateBackButton()
	h.sender.SendResponse(userID, messageID, result, &keyboard)

	// 如果还有订阅，1秒后刷新删除选项
	go func() {
		time.Sleep(time.Second)
		subscriptions, err := getSubscriptionsForUser(userID)
		if err == nil && len(subscriptions) > 0 {
			h.showDeleteSubscriptions(userID, messageID)
		}
	}()
}

// 格式化方法
func (h *UserActionHandler) formatKeywordsList(keywords []string) string {
	var rows []string
	var currentRow []string

	for i, kw := range keywords {
		currentRow = append(currentRow, fmt.Sprintf("%d.<code>%s</code>", i+1, kw))
		if i == len(keywords)-1 {
			rows = append(rows, strings.Join(currentRow, "  "))
		}
	}

	return fmt.Sprintf("📋 你的关键词列表（共 %d 个）：\n\n%s", len(keywords), strings.Join(rows, "\n"))
}

func (h *UserActionHandler) formatSubscriptionsList(subscriptions []SubscriptionInfo) string {
	var subList []string
	for i, sub := range subscriptions {
		subList = append(subList, fmt.Sprintf("订阅%d.<code>%s</code>\n%s", i+1, sub.Name, sub.URL))
	}
	return fmt.Sprintf("📰 你的订阅列表（共 %d 个）：\n\n%s", len(subscriptions), strings.Join(subList, "\n"))
}

// 全局实例
var (
	messageSender    *MessageSender
	databaseOperator *DatabaseOperator
	actionHandler    *UserActionHandler
)

// main 主函数
func main() {
	var err error

	// 加载配置
	globalConfig, err = loadConfig()
	if err != nil {
		log.Fatal("加载配置文件失败:", err)
	}
	asciiArt := `
    _    _     ____            _ 
   / \  | |__ | __ ) _   _  __(_)
  / _ \ | '_ \|  _ \| | | |/ _| |
 / ___ \| |_) | |_) | |_| | (_| |
/_/   \_\_.__/|____/ \__,_|\__,_|
                                 
`
	intro := fmt.Sprintf(`%s
欢迎使用 TG RSS Bot
版本: %s
构建时间: %s
作者: AbBai (阿布白)
源码仓库: https://github.com/IonRh/TGBot_RSS
简介: TGBot_RSS 是一个灵活的利用TGBot信息推送订阅RSS的工具。
探索更多：https://github.com/IonRh`, asciiArt, version, buildTime)
	logMessage("info", fmt.Sprintf(intro+"\n"))
	// 初始化日志系统
	logMessage("info", "RSS Bot 启动中...")

	// 初始化数据库连接
	db, err = sql.Open("sqlite3", fmt.Sprintf("%s?cache=shared&mode=rwc&_timeout=30000", DBFile))
	if err != nil {
		log.Fatal("连接数据库失败:", err)
	}
	defer db.Close()

	// 设置数据库连接池参数
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// 初始化数据库表结构
	if err := initDatabase(); err != nil {
		log.Fatal("初始化数据库失败:", err)
	}

	// 创建带代理的 HTTP 客户端
	client := createHTTPClient(globalConfig.ProxyURL)

	// 使用自定义客户端创建 Telegram Bot API 客户端
	bot, err = tgbotapi.NewBotAPIWithClient(globalConfig.BotToken, tgbotapi.APIEndpoint, client)
	if err != nil {
		log.Fatal("创建Bot失败:", err)
	}

	// 设置调试模式
	bot.Debug = false
	logMessage("info", fmt.Sprintf("Bot已启动，授权账户: %s", bot.Self.UserName))

	// 初始化统一组件
	messageSender = NewMessageSender(bot)
	databaseOperator = NewDatabaseOperator(db)
	actionHandler = NewUserActionHandler(messageSender, databaseOperator)

	// 启动RSS监控协程
	go startRSSMonitor()

	// 配置更新获取参数
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	// 获取更新通道
	updates := bot.GetUpdatesChan(u)

	// 处理消息更新
	//logMessage("info", "开始处理消息...")
	for update := range updates {
		go func(update tgbotapi.Update) {
			// 异常恢复处理
			defer func() {
				if r := recover(); r != nil {
					logMessage("error", fmt.Sprintf("处理更新时发生panic: %v", r))
				}
			}()

			// 根据更新类型分发处理
			if update.Message != nil {
				handleMessage(update.Message)
			} else if update.CallbackQuery != nil {
				handleCallbackQuery(update.CallbackQuery)
			}
		}(update)
	}
}

// 处理普通消息
func handleMessage(message *tgbotapi.Message) {
	userID := message.From.ID

	defer func() {
		if r := recover(); r != nil {
			logMessage("error", fmt.Sprintf("处理消息时发生panic: %v", r), userID)
			sendMessage(userID, "处理消息时发生错误，请稍后重试")
		}
	}()

	// 处理命令
	if message.IsCommand() {
		handleCommand(message)
		return
	}

	// 检查用户状态
	state := getUserState(userID)
	if state != nil {
		handleStateMessage(message, state)
		return
	}

	// 处理回复消息（向后兼容）
	if message.ReplyToMessage != nil {
		replyText := message.ReplyToMessage.Text
		switch {
		case strings.Contains(replyText, "请输入要添加的关键词"):
			handleKeywordInput(message)
			return
		case strings.Contains(replyText, "请按以下格式输入RSS订阅信息"):
			handleSubscriptionInput(message)
			return
		}
	}

	// 默认回复
	//htmlExample := "👋 <b>NodeSeek 新帖送达</b>\n" +
	//	"<a href=\"https://markdown.com.cn\">HTML语法示例</a>\n" +
	//	"🕒 2025-05-28 21:42:19\n" +
	//	"· 支持列表\n" +
	//	"· 支持<b>粗体</b>和<i>斜体</i>\n" +
	//	"· 支持<code>代码块</code>"
	htmlExample := fmt.Sprintf("请使用 /start 查看菜单或 /help 获取帮助")
	sendHTMLMessage(userID, htmlExample)
	//sendMessage(userID, "请使用 /start 查看功能菜单")
}

// 处理状态消息
func handleStateMessage(message *tgbotapi.Message, state *UserState) {
	userID := message.From.ID

	switch state.Action {
	case "add_keyword":
		handleKeywordInput(message)
	case "add_subscription":
		handleSubscriptionInput(message)
	default:
		logMessage("warn", fmt.Sprintf("未知的用户状态: %s", state.Action), userID)
		clearUserState(userID)
		sendMessage(userID, "操作已取消，请重新开始")
	}
}

// 处理关键词输入
func handleKeywordInput(message *tgbotapi.Message) {
	userID := message.From.ID
	text := strings.TrimSpace(message.Text)
	if text == "" {
		messageSender.SendError(userID, 0, "❌ 请输入有效的关键词")
		return
	}

	keywords := strings.Fields(text)
	if len(keywords) == 0 {
		messageSender.SendError(userID, 0, "❌ 请输入有效的关键词")
		return
	}

	actionHandler.HandleAction(userID, 0, "keyword", "add", keywords...)
}

// 处理订阅输入
func handleSubscriptionInput(message *tgbotapi.Message) {
	userID := message.From.ID
	parts := strings.SplitN(strings.TrimSpace(message.Text), " ", 3)

	if len(parts) != 3 {
		messageSender.SendError(userID, 0, "❌ 格式错误！请按照以下格式输入：\nURL 名称\n例如：https://example.com/feed 科技新闻")
		return
	}

	actionHandler.HandleAction(userID, 0, "subscription", "add", parts[0], parts[1], parts[2])
}

// 显示主菜单
func showMainMenu(userID int64, from string, messageID int) {
	stats, err := getUserStats(userID)
	//fmt.Println(userID, from)
	if err != nil {
		logMessage("error", fmt.Sprintf("获取用户统计失败: %v", err), userID)
		stats = &UserStats{}
	}
	pushstats := GetPushStatsInfo()
	menuText := fmt.Sprintf(`👋 欢迎使用 TGBot_RSS 订阅机器人！

👥 %s(<code>%d</code>)：
📰 订阅数：%d    🔍关键词数：%d

%s
1️⃣ 订阅管理：增加/删除/查看 RSS 源
2️⃣ 关键词管理：增加/删除/查看 关键词

请选择以下操作：`,
		from, userID, stats.SubscriptionCount, stats.KeywordCount, pushstats)

	keyboard := createMainMenuKeyboard()
	messageSender.SendHTMLResponse(userID, messageID, menuText, &keyboard)
}

// 显示帮助信息
func showHelp(userID int64, messageID int) {
	count := downloadcounnt()
	helpText := fmt.Sprintf(`🤖 RSS订阅机器人
📰 TGBot_RSS 当前下载：%d 次

📝 <b>使用帮助（不推送可尝试以下方式解决）</b>

🔤 <b>关键词基础</b>
• 支持中英文，可用逗号(,)分隔多个关键词
• 可使用正则表达式进行高级匹配

🎯 <b>高级匹配</b>
• <code>*</code> 可匹配任意字符
• <code>-关键词</code> 表示屏蔽关键词
• 示例：<code>你*帅*</code> 可匹配 "你好帅呀！" 等
• 示例：<code>-你好丑</code> 可屏蔽包含 "你好丑" 的内容

🎯 <b>匹配范围</b>
• 默认只匹配标题，如需更精确控制可使用以下前缀：
• #t 关键词 - 只匹配标题
• #c 关键词 - 只匹配描述内容
• #a 关键词 - 匹配标题和描述
• 示例：<code>#t技术</code> 只在标题中匹配"技术"

📡 <b>RSS过滤(可配合高级匹配使用)</b>
• <code>关键词+RSS名称</code> 只匹配指定RSS源
• 示例：<code>技术+科技新闻</code> 只匹配名为 "科技新闻" 的RSS源
• 不加"+RSS名称"则匹配所有订阅源

📦 源码仓库: github.com/IonRh/TGBot_RSS
🔧 问题反馈: https://t.me/IonMagic`, count)

	keyboard := CreateBackButton()
	messageSender.SendHTMLResponse(userID, messageID, helpText, &keyboard, true)
}

// handleCommand 处理命令消息
// 根据命令类型执行相应操作
func handleCommand(message *tgbotapi.Message) {
	userID := message.From.ID
	//fmt.Println(globalConfig.ADMINIDS)
	if userID == globalConfig.ADMINIDS {
		logMessage("debug", fmt.Sprintf("管理员用户使用命令: %s", message.Command()), userID)
	} else if globalConfig.ADMINIDS == 0 {
		logMessage("debug", fmt.Sprintf("全用户可用，用户尝试使用命令: %s", message.Command()), userID)
	} else {
		logMessage("warn", fmt.Sprintf("非管理员用户尝试使用命令: %s", message.Command()), userID)
		sendMessage(userID, "你没有权限使用此命令")
		return
	}
	from := message.From.FirstName + " " + message.From.LastName
	command := message.Command()

	logMessage("debug", fmt.Sprintf("收到命令: %s", command), userID)

	switch command {
	case "start":
		// 清除可能的旧状态
		clearUserState(userID)
		// 发送欢迎消息和主菜单
		showMainMenu(userID, from, 0)

	case "help":
		// 发送帮助信息
		showHelp(userID, 0)

	// 可添加更多命令处理
	default:
		// 未知命令
		sendMessage(userID, fmt.Sprintf("未知命令: %s\n请使用 /start 查看菜单或 /help 获取帮助", command))
	}
}

// handleCallbackQuery 处理回调查询
// 处理来自内联键盘按钮的点击
func handleCallbackQuery(callbackQuery *tgbotapi.CallbackQuery) {
	userID := callbackQuery.From.ID
	from := callbackQuery.From.FirstName + " " + callbackQuery.From.LastName
	data := callbackQuery.Data
	messageID := callbackQuery.Message.MessageID

	// 异常恢复处理
	defer func() {
		if r := recover(); r != nil {
			logMessage("error", fmt.Sprintf("处理回调查询时发生panic: %v", r), userID)
		}
	}()

	// 回应回调查询以停止按钮加载动画
	callback := tgbotapi.NewCallback(callbackQuery.ID, "")
	if _, err := bot.Request(callback); err != nil {
		logMessage("error", fmt.Sprintf("回应回调查询失败: %v", err), userID)
	}

	// 清除用户状态（除非是需要输入的操作）
	if data != "add_keyword" && data != "add_subscription" {
		clearUserState(userID)
	}

	// 使用统一的处理器
	switch {
	case data == "back_to_menu":
		showMainMenu(userID, from, messageID)

	case data == "add_keyword":
		actionHandler.HandleAction(userID, messageID, "keyword", "add_prompt")

	case data == "view_keywords":
		actionHandler.HandleAction(userID, messageID, "keyword", "view")

	case data == "delete_keyword":
		actionHandler.HandleAction(userID, messageID, "keyword", "delete_list")

	case data == "add_subscription":
		actionHandler.HandleAction(userID, messageID, "subscription", "add_prompt")

	case data == "view_subscriptions":
		actionHandler.HandleAction(userID, messageID, "subscription", "view")

	case data == "delete_subscription":
		actionHandler.HandleAction(userID, messageID, "subscription", "delete_list")

	case data == "help":
		showHelp(userID, messageID)

	case strings.HasPrefix(data, "del_kw_"):
		keyword := strings.TrimPrefix(data, "del_kw_")
		actionHandler.HandleAction(userID, messageID, "keyword", "delete", keyword)

	case strings.HasPrefix(data, "del_sub_"):
		subscription := strings.TrimPrefix(data, "del_sub_")
		actionHandler.HandleAction(userID, messageID, "subscription", "delete", subscription)

	default:
		logMessage("warn", fmt.Sprintf("未知的回调数据: %s", data), userID)
		messageSender.SendError(userID, messageID, "未知的操作，请重试")
	}
}

// createMainMenuKeyboard 创建主菜单键盘
// 返回带有所有功能按钮的内联键盘
func createMainMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		// 关键词管理行
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 添加关键词", "add_keyword"),
			tgbotapi.NewInlineKeyboardButtonData("📋 查看关键词", "view_keywords"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑️ 删除关键词", "delete_keyword"),
		),
		// 订阅管理行
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ 添加订阅", "add_subscription"),
			tgbotapi.NewInlineKeyboardButtonData("📰 查看订阅", "view_subscriptions"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑️ 删除订阅", "delete_subscription"),
			tgbotapi.NewInlineKeyboardButtonData("ℹ️ 关于", "help"),
		),
	)
}

// 发送消息函数

// sendMessage 发送普通文本消息
func sendMessage(userID int64, text string) {
	msg := tgbotapi.NewMessage(userID, text)
	if _, err := bot.Send(msg); err != nil {
		logMessage("error", fmt.Sprintf("发送消息失败: %v", err), userID)
	}
}

// sendHTMLMessage 发送HTML格式的消息
func sendHTMLMessage(userID int64, text string) {
	msg := tgbotapi.NewMessage(userID, text)
	msg.ParseMode = "HTML" // 设置解析模式为HTML
	if _, err := bot.Send(msg); err != nil {
		logMessage("error", fmt.Sprintf("发送HTML消息失败: %v", err), userID)
	}
}

func sendPhotoMessage(userID int64, photoURL, caption string) {
	msg := tgbotapi.NewPhoto(userID, tgbotapi.FileURL(photoURL))
	msg.Caption = caption
	msg.ParseMode = "HTML" // 支持在说明文字中使用HTML格式

	if _, err := bot.Send(msg); err != nil {
		logMessage("error", fmt.Sprintf("发送图片消息失败: %v", err), userID)
		// 如果发送图片失败，尝试发送纯文本消息
		fallbackMsg := fmt.Sprintf("图片: %s\n\n%s", photoURL, caption)
		sendHTMLMessage(userID, fallbackMsg)
	}
}

// 数据库操作函数
func initDatabase() error {
	// 表定义
	tables := map[string]string{
		"subscriptions": `CREATE TABLE IF NOT EXISTS subscriptions (
			subscription_id INTEGER PRIMARY KEY AUTOINCREMENT, -- 订阅ID
			rss_url TEXT NOT NULL,                             -- RSS源URL
			rss_name TEXT NOT NULL UNIQUE,                     -- 订阅名称（唯一）
			users TEXT NOT NULL DEFAULT ',',                   -- 订阅用户列表，格式为",user_id,user_id,"
			channel INTEGER DEFAULT 0                       -- 是否推送给所有用户(0/1)
		)`,
		"user_keywords": `CREATE TABLE IF NOT EXISTS user_keywords (
			user_id INTEGER PRIMARY KEY,                       -- 用户ID
			keywords TEXT NOT NULL DEFAULT '[]'               -- 关键词列表，JSON格式
		)`,
		"feed_data": `CREATE TABLE IF NOT EXISTS feed_data (
			rss_name TEXT PRIMARY KEY,                         -- 订阅名称
			last_update_time TEXT, -- 最后更新时间
			latest_title TEXT DEFAULT ''                      -- 最新文章标题
		)`,
	}

	// 创建表
	for name, tablesql := range tables {
		if err := withDB(func(db *sql.DB) error {
			_, err := db.Exec(tablesql)
			return err
		}); err != nil {
			return fmt.Errorf("创建表 %s 失败: %v", name, err)
		}
		logMessage("debug", fmt.Sprintf("数据库表 %s 已创建或已存在", name))
	}

	// 索引定义
	indexes := []struct {
		name string
		sql  string
	}{
		{
			name: "idx_subscriptions_users",
			sql:  "CREATE INDEX IF NOT EXISTS idx_subscriptions_users ON subscriptions(users)",
		},
		{
			name: "idx_feed_data_update_time",
			sql:  "CREATE INDEX IF NOT EXISTS idx_feed_data_update_time ON feed_data(last_update_time)",
		},
	}

	// 创建索引
	for _, index := range indexes {
		if err := withDB(func(db *sql.DB) error {
			_, err := db.Exec(index.sql)
			return err
		}); err != nil {
			logMessage("warn", fmt.Sprintf("创建索引 %s 失败: %v", index.name, err))
			// 索引创建失败不阻止程序运行
		} else {
			logMessage("debug", fmt.Sprintf("索引 %s 已创建或已存在", index.name))
		}
	}

	logMessage("info", "数据库初始化完成")
	return nil
}

func getKeywordsForUser(userID int64) ([]string, error) {
	var keywordsStr string
	var keywords []string

	err := withDB(func(db *sql.DB) error {
		return db.QueryRow("SELECT keywords FROM user_keywords WHERE user_id = ?", userID).Scan(&keywordsStr)
	})

	if err == sql.ErrNoRows {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(keywordsStr), &keywords); err != nil {
		return nil, err
	}
	return keywords, nil
}

func addKeywordsForUser(userID int64, newKeywords []string) (string, error) {
	existingKeywords, err := getKeywordsForUser(userID)
	if err != nil {
		return "", err
	}
	//fmt.Println(existingKeywords)
	//fmt.Println(newKeywords)
	// 去重合并
	keywordMap := make(map[string]bool)
	for _, k := range existingKeywords {
		keywordMap[k] = true
	}
	//fmt.Println(keywordMap)

	// 处理逗号分隔的关键词
	var processedKeywords []string
	for _, k := range newKeywords {
		// 替换中式逗号为美式逗号
		k = strings.ReplaceAll(k, "，", ",")
		// 按逗号分割
		if strings.Contains(k, ",") {
			parts := strings.Split(k, ",")
			for _, part := range parts {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					processedKeywords = append(processedKeywords, trimmed)
				}
			}
		} else {
			if trimmed := strings.TrimSpace(k); trimmed != "" {
				processedKeywords = append(processedKeywords, trimmed)
			}
		}
	}

	// 添加新关键词并去重
	var addedCount int
	for _, k := range processedKeywords {
		if !keywordMap[k] {
			keywordMap[k] = true
			addedCount++
		}
	}

	// 如果没有新增关键词
	if addedCount == 0 {
		return "❌ 没有新增关键词，可能全部已存在", nil
	}

	// 将map转换回slice
	var finalKeywords []string
	for k := range keywordMap {
		finalKeywords = append(finalKeywords, k)
	}

	// 对关键词进行排序，使显示更有序
	sort.Strings(finalKeywords)

	// 转换为JSON格式
	keywordsJSON, err := json.Marshal(finalKeywords)
	if err != nil {
		return "", err
	}

	// 更新数据库
	err = withDB(func(db *sql.DB) error {
		// 检查是否已存在记录
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM user_keywords WHERE user_id = ?", userID).Scan(&count)
		if err != nil {
			return err
		}

		if count > 0 {
			// 更新现有记录
			_, err = db.Exec("UPDATE user_keywords SET keywords = ? WHERE user_id = ?",
				string(keywordsJSON), userID)
		} else {
			// 插入新记录
			_, err = db.Exec("INSERT INTO user_keywords (user_id, keywords) VALUES (?, ?)",
				userID, string(keywordsJSON))
		}
		return err
	})

	if err != nil {
		return "", err
	}

	// 构建关键词列表字符串
	// 每行显示4个关键词
	var rows []string
	var currentRow []string

	for i, kw := range finalKeywords {
		currentRow = append(currentRow, fmt.Sprintf("%d.%s", i+1, kw))

		// 如果当前行已满4个或者是最后一个关键词，则添加到行列表中
		if i == len(finalKeywords)-1 {
			rows = append(rows, strings.Join(currentRow, "  "))
		}
	}

	// 返回成功消息，并列出所有关键词
	logMessage("info", fmt.Sprintf("✅ 成功添加 %d 个关键词 当前共有 %d 个关键词 📋 关键词列表：%s", addedCount, len(finalKeywords), strings.Join(rows, "\n")))
	return fmt.Sprintf("✅ 成功添加 %d 个关键词\n当前共有 %d 个关键词\n\n📋 关键词列表：\n%s",
		addedCount, len(finalKeywords), strings.Join(rows, "\n")), nil
}

func removeKeywordForUser(userID int64, keyword string) (string, error) {
	keywords, err := getKeywordsForUser(userID)
	if err != nil {
		return "", err
	}

	var newKeywords []string
	found := false
	for _, k := range keywords {
		if k != keyword {
			newKeywords = append(newKeywords, k)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Sprintf("❌ 关键词 \"%s\" 不存在", keyword), nil
	}

	keywordsJSON, err := json.Marshal(newKeywords)
	if err != nil {
		return "", err
	}
	keywordsJ := string(keywordsJSON)
	if string(keywordsJSON) == "null" {
		keywordsJ = "[]" // 确保删除后不会存储空字符串
	}
	//fmt.Println(string(keywordsJSON))
	err = withDB(func(db *sql.DB) error {
		_, err := db.Exec("UPDATE user_keywords SET keywords = ? WHERE user_id = ?",
			keywordsJ, userID)
		return err
	})

	if err != nil {
		return "", err
	}

	// 如果没有剩余关键词，直接返回删除成功的消息
	if len(newKeywords) == 0 {
		return fmt.Sprintf("✅ 关键词 \"%s\" 已删除\n当前没有关键词", keyword), nil
	}

	// 对关键词进行排序，使显示更有序
	sort.Strings(newKeywords)

	// 构建关键词列表字符串
	// 每行显示6个关键词
	var rows []string
	var currentRow []string

	for i, kw := range newKeywords {
		currentRow = append(currentRow, fmt.Sprintf("%d.%s", i+1, kw))

		// 如果当前行已满6个或者是最后一个关键词，则添加到行列表中
		if i == len(newKeywords)-1 {
			rows = append(rows, strings.Join(currentRow, "  "))
		}
	}

	return fmt.Sprintf("✅ 关键词 \"%s\" 已删除\n当前剩余 %d 个关键词\n\n📋 关键词列表：\n%s",
		keyword, len(newKeywords), strings.Join(rows, "\n")), nil
}

func getSubscriptionsForUser(userID int64) ([]SubscriptionInfo, error) {
	var subscriptions []SubscriptionInfo

	err := withDB(func(db *sql.DB) error {
		// 获取所有订阅
		rows, err := db.Query(`SELECT rss_name, rss_url, users FROM subscriptions`)

		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var sub SubscriptionInfo
			var usersStr string
			if err := rows.Scan(&sub.Name, &sub.URL, &usersStr); err != nil {
				continue
			}

			// 解析用户列表
			var users []int64
			if err := json.Unmarshal([]byte(usersStr), &users); err != nil {
				// 如果解析失败，可能是旧格式，尝试转换
				if strings.HasPrefix(usersStr, ",") && strings.HasSuffix(usersStr, ",") {
					userIDs := strings.Split(strings.Trim(usersStr, ","), ",")
					for _, userIDStr := range userIDs {
						if userIDStr == "" {
							continue
						}
						if uid, err := strconv.ParseInt(userIDStr, 10, 64); err == nil && uid == userID {
							// 旧格式匹配到了用户
							subscriptions = append(subscriptions, sub)
							break
						}
					}
				}
				continue
			}

			// 检查用户是否在列表中
			for _, uid := range users {
				if uid == userID {
					subscriptions = append(subscriptions, sub)
					break
				}
			}
		}
		return nil
	})

	return subscriptions, err
}

func removeSubscriptionForUser(userID int64, subscriptionName string) (string, error) {
	var result string

	err := withDB(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		var usersStr string
		err = tx.QueryRow("SELECT users FROM subscriptions WHERE rss_name = ?", subscriptionName).Scan(&usersStr)
		if err != nil {
			return err
		}

		// 解析用户列表
		var users []int64
		var newUsers []int64
		if err := json.Unmarshal([]byte(usersStr), &users); err != nil {
			// 如果解析失败，可能是旧格式，尝试转换
			if strings.HasPrefix(usersStr, ",") && strings.HasSuffix(usersStr, ",") {
				userStrs := strings.Split(strings.Trim(usersStr, ","), ",")
				for _, userStr := range userStrs {
					if userStr == "" {
						continue
					}
					if uid, err := strconv.ParseInt(userStr, 10, 64); err == nil {
						if uid != userID {
							newUsers = append(newUsers, uid)
						}
					}
				}

				// 如果是旧格式，转换为新格式
				usersJSON, err := json.Marshal(newUsers)
				if err != nil {
					return err
				}

				if len(newUsers) == 0 {
					// 删除整个订阅
					_, err = tx.Exec("DELETE FROM subscriptions WHERE rss_name = ?", subscriptionName)
					if err == nil {
						_, err = tx.Exec("DELETE FROM feed_data WHERE rss_name = ?", subscriptionName)
					}
					result = fmt.Sprintf("✅ 订阅 \"%s\" 已被完全删除", subscriptionName)
				} else {
					// 更新用户列表
					_, err = tx.Exec("UPDATE subscriptions SET users = ? WHERE rss_name = ?", string(usersJSON), subscriptionName)
					result = fmt.Sprintf("✅ 你已取消订阅 \"%s\"", subscriptionName)
				}

				if err != nil {
					return err
				}

				return tx.Commit()
			}
			return err
		}

		// 过滤掉要删除的用户
		for _, uid := range users {
			if uid != userID {
				newUsers = append(newUsers, uid)
			}
		}

		usersJSON, err := json.Marshal(newUsers)
		if err != nil {
			return err
		}

		if len(newUsers) == 0 {
			// 删除整个订阅
			_, err = tx.Exec("DELETE FROM subscriptions WHERE rss_name = ?", subscriptionName)
			if err == nil {
				_, err = tx.Exec("DELETE FROM feed_data WHERE rss_name = ?", subscriptionName)
			}
			result = fmt.Sprintf("✅ 订阅 \"%s\" 已被完全删除", subscriptionName)
		} else {
			// 更新用户列表
			_, err = tx.Exec("UPDATE subscriptions SET users = ? WHERE rss_name = ?", string(usersJSON), subscriptionName)
			result = fmt.Sprintf("✅ 你已取消订阅 \"%s\"", subscriptionName)
		}

		if err != nil {
			return err
		}

		return tx.Commit()
	})

	return result, err
}

func getUserStats(userID int64) (*UserStats, error) {
	stats := &UserStats{}

	err := withDB(func(db *sql.DB) error {
		// 获取用户订阅数
		subscriptions, err := getSubscriptionsForUser(userID)
		stats.SubscriptionCount = len(subscriptions)

		// 获取用户关键词数
		keywords, err := getKeywordsForUser(userID)
		if err == nil {
			stats.KeywordCount = len(keywords)
		}

		// 获取总用户数
		userSet := make(map[int64]bool)

		// 从user_keywords表获取用户
		rows, err := db.Query("SELECT user_id FROM user_keywords")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var uid int64
				if err := rows.Scan(&uid); err == nil {
					userSet[uid] = true
				}
			}
		}

		// 从subscriptions表获取用户
		rows, err = db.Query("SELECT users FROM subscriptions")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var users string
				if err := rows.Scan(&users); err != nil {
					continue
				}
				userIDs := strings.Split(strings.Trim(users, ","), ",")
				for _, userIDStr := range userIDs {
					if userIDStr == "" {
						continue
					}
					if uid, err := strconv.ParseInt(userIDStr, 10, 64); err == nil {
						userSet[uid] = true
					}
				}
			}
		}
		return nil
	})
	//fmt.Println(stats)
	return stats, err
}

func validateAndProcessSubscription(feedURL, name, channel string, userID int64) error {
	// 验证URL格式
	parsedURL, err := url.Parse(feedURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return fmt.Errorf("无效的URL格式，请使用http或https开头的完整URL")
	}

	// 验证RSS源有效性
	if valid, errMsg := verifyRSSFeed(feedURL); !valid {
		return fmt.Errorf("RSS源验证失败: %s", errMsg)
	}

	return withDB(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// 检查订阅是否已存在
		var existingUsersStr string
		err = tx.QueryRow("SELECT users FROM subscriptions WHERE rss_url = ? OR rss_name = ?", feedURL, name).Scan(&existingUsersStr)

		if err == sql.ErrNoRows {
			// 新订阅
			usersJSON, err := json.Marshal([]int64{userID})
			if err != nil {
				return err
			}

			_, err = tx.Exec(`
				INSERT INTO subscriptions (rss_url, rss_name, users, channel)
				VALUES (?, ?, ?, ?)
			`, feedURL, name, string(usersJSON), channel)
			if err != nil {
				return err
			}

			// 初始化 feed_data 记录
			_, err = tx.Exec(`
				INSERT INTO feed_data (rss_name, last_update_time) VALUES (?, CURRENT_TIMESTAMP)
			`, name)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err // 返回其他错误
		} else {
			// 订阅已存在，更新用户列表
			var existingUsers []int64
			if err := json.Unmarshal([]byte(existingUsersStr), &existingUsers); err != nil {
				return err
			}

			// 检查用户是否已订阅
			for _, uid := range existingUsers {
				if uid == userID {
					return fmt.Errorf("你已经订阅了这个RSS源")
				}
			}

			// 添加用户到现有订阅
			existingUsers = append(existingUsers, userID)
			usersJSON, err := json.Marshal(existingUsers)
			if err != nil {
				return err
			}

			_, err = tx.Exec("UPDATE subscriptions SET users = ? WHERE rss_url = ?", string(usersJSON), feedURL)
			if err != nil {
				return err
			}
		}

		return tx.Commit()
	})
}

func verifyRSSFeed(feedURL string) (bool, string) {
	client := createHTTPClient(globalConfig.ProxyURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return false, "创建请求失败"
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSS Bot/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("HTTP状态码错误: %d", resp.StatusCode)
	}

	// 读取部分内容进行检测
	body := make([]byte, 8192)
	n, _ := io.ReadFull(resp.Body, body)
	content := string(body[:n])

	if strings.Contains(content, "<rss") || strings.Contains(content, "<feed") ||
		strings.Contains(content, "<?xml") {
		return true, ""
	}

	return false, "未检测到有效的RSS/Atom格式"
}

// RSS监控功能
func startRSSMonitor() {
	//logMessage("info", "RSS监控已启动")
	ticker := time.NewTicker(time.Duration(globalConfig.Cycletime) * time.Minute)
	defer ticker.Stop()
	db, err := sql.Open("sqlite3", "tgbot.db")
	if err != nil {
		logMessage("error", fmt.Sprintf("连接数据库失败: %v", err))
		os.Exit(1)
	}
	defer db.Close()
	checkAllRSS(db)
	logMessage("info", fmt.Sprintf("TGBot已启动，每%d分钟检查一次RSS", globalConfig.Cycletime))
	for {
		select {
		case <-ticker.C:
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logMessage("error", fmt.Sprintf("RSS监控发生panic: %v", r))
					}
				}()
				checkAllRSS(db)
			}()
		}
	}
}

// splitMessage 将长文本分割成多个片段
func splitMessage(text string, maxLength int) []string {
	var chunks []string
	// 文本过长时循环分割
	for len(text) > maxLength {
		chunk := text[:maxLength]
		// 尝试在换行符处分割
		lastNewline := strings.LastIndex(chunk, "\n")
		if lastNewline != -1 && lastNewline > maxLength/2 {
			// 在换行处分割
			chunk = text[:lastNewline]
			text = text[lastNewline+1:]
		} else {
			// 没有合适的换行符，直接按长度分割
			text = text[maxLength:]
		}
		chunks = append(chunks, chunk)
	}

	// 添加剩余文本
	if len(text) > 0 {
		chunks = append(chunks, text)
	}

	return chunks
}
func sendother(message string) {
	// 使用全局配置而不是创建新的空指针
	if globalConfig.Pushinfo == "" {
		return
	}
	encodedInfo := url.QueryEscape(message)
	tgURL := fmt.Sprintf(globalConfig.Pushinfo+"%s", encodedInfo)

	// 使用与其他HTTP请求相同的客户端配置
	client := createHTTPClient(globalConfig.ProxyURL)
	resp, err := client.Get(tgURL)
	if err != nil {
		logMessage("error", fmt.Sprintf("推送消息失败: %v", err))
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logMessage("error", fmt.Sprintf("推送消息失败, 状态码: %d, 响应内容: %s", resp.StatusCode, string(body)))
		return
	}
	logMessage("debug", fmt.Sprintf("成功推送，响应结果: %s", resp.Status))
}

type Asset struct {
	DownloadCount int `json:"download_count"`
}

type Release struct {
	Assets []Asset `json:"assets"`
}

func downloadcounnt() int {
	owner := "IonRh"
	repo := "TGBot_RSS"
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)

	client := createHTTPClient(globalConfig.ProxyURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return 1
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching releases: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		fmt.Printf("Error decoding JSON: %v\n", err)
		return 1
	}

	totalDownloads := 0
	for _, release := range releases {
		for _, asset := range release.Assets {
			totalDownloads += asset.DownloadCount
		}
	}
	//
	//fmt.Printf("Total Downloads: %d\n", totalDownloads)
	return totalDownloads
}
