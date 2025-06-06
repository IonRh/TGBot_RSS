# TGBot RSS
```
    _    _     ____            _ 
   / \  | |__ | __ ) _   _  __(_)
  / _ \ | '_ \|  _ \| | | |/ _| |
 / ___ \| |_) | |_) | |_| | (_| |
/_/   \_\_.__/|____/ \__,_|\__,_|
```

TGBot RSS 是一个基于 Telegram Bot 的 RSS 订阅工具，支持关键词过滤、多用户订阅和自定义推送格式。通过 TGBot RSS，你可以轻松获取感兴趣的 RSS 内容，并通过 Telegram 接收实时推送。

长期维护：https://t.me/JK_550w_bot

交流群：https://t.me/IonMagic

全推送频道：https://t.me/Push_Forum
## 功能特点

- 🔍 **关键词过滤**：支持添加多个关键词，只推送包含关键词的内容
- 🚫 **屏蔽功能**：支持使用 `-关键词` 格式屏蔽不想看到的内容
- 🔄 **定时更新**：自动定期检查 RSS 源的更新
- 👥 **多用户支持**：支持多个用户订阅同一个 RSS 源
- 📊 **推送统计**：记录并显示每日推送数据
- 🖼️ **图片支持**：自动提取 RSS 内容中的图片并发送
- 🔗 **HTML 支持**：保留 Telegram 支持的 HTML 标签格式
- 🔒 **代理支持**：可配置代理服务器访问被墙的 RSS 源

| 主面板   | 推送样式 | 关于    |
|--------|------|---------|
| ![image](https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/Image/2afc6ebcf12db6f36af25877e7e24f72.png)   | ![image](https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/Image/2025-06-06%20225016.png)   | ![image](https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/Image/2025-06-06%20223327.png)   |

### 安装步骤

1. 执行：

```
curl -sL https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/TGBot_RSS.sh | bash
```

2. 根据自己的需求修改 `config.json` 配置文件
3. 测试可执行文件 `./TGBot_RSS`
4. 后台运行可执行文件 `nohup ./TGBot_RSS > /dev/null 2>&1 &`


### 配置说明：
- `BotToken`: Telegram Bot 的 API 令牌，从 @BotFather 获取
- `ADMINIDS`: 管理员用户 ID，设置为 0 表示所有用户可用，自用建议设置为自己UID如：`60xxxxxxxx`
- `Cycletime`: RSS 检查周期，单位为分钟,建议为1
- `Debug`: 是否开启调试模式
- `ProxyURL`: 代理服务器 URL，例如 `http://127.0.0.1:7890`，默认为空则不使用代理
- `Pushinfo`: 额外推送接口 URL，可设置为微信机器人之类的消息推送接口如此格式`https://xxxx.xxxxx.xxx/send_msg?access_token=xxxxxxx&msgtype=xxxx&touser=xxxxx&content=`
此接口将与TGBot收到同等消息，可实现TG控制Bot关键词，其他链接，接收识别到关键词的帖子
## 使用指南

### 基本命令

- `/start` - 显示主菜单
- `/help` - 显示帮助信息

### 添加订阅

1. 在主菜单中点击 "➕ 添加订阅"
2. 按照格式输入 RSS 信息：`URL 名称 TG频道用0常规用1`
   - 例如：`https://example.com/feed 科技新闻 0`
![image](https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/Image/2025-06-06%20223402.png)
### 添加关键词

1. 在主菜单中点击 "📝 添加关键词"
2. 输入关键词，多个关键词可用空格或逗号分隔
3. 支持以下格式：
   - 普通关键词：`科技`
   - 通配符匹配：`科技*新闻`（匹配"科技最新新闻"等）
   - 屏蔽关键词：`-广告`（屏蔽包含"广告"的内容）
![image](https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/Image/2025-06-06%20223348.png)
### 查看和删除

- 点击 "📋 查看关键词" 或 "📰 查看订阅" 可以查看已添加的内容
- 点击 "🗑️ 删除关键词" 或 "🗑️ 删除订阅" 可以删除不需要的内容

## 数据库结构

TGBot RSS 使用 SQLite 数据库存储数据，包含以下表：

- `subscriptions`: 存储 RSS 订阅信息
- `user_keywords`: 存储用户关键词
- `feed_data`: 存储 RSS 源的最后更新时间和最新标题

## 高级功能

### 关键词匹配规则

- 支持普通文本匹配
- 支持通配符 `*` 匹配任意字符
- 支持使用 `-` 前缀屏蔽特定内容

## 常见问题

- 如存在问题，打开debug，再issue中反馈
- 暂不开源，无任何后门，挖矿，等危害计算机行为，可抓包获取外连网址。
## 联系方式

- 问题反馈：https://t.me/IonMagic
- 项目仓库：https://github.com/IonRh/TGBot_RSS 
