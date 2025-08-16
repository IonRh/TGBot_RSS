#!/bin/bash

export TZ='Asia/Shanghai'

# 检查/root是否有文件
if [ "$(ls -A /root/)" ]; then
	# 有文件则执行TGBot_RSS
	cd /root && ./TGBot_RSS > /dev/null 2>&1 &
else
	# 无文件则移动/app/到/root
	mv /app/config.json /root/
    mv /app/TGBot_RSS /root/
    cd /root/
    sed -i "s/\"BotToken\": \".*\"/\"BotToken\": \"$BotToken\"/g" config.json
    sed -i "s/\"ADMINIDS\": [0-9]*/\"ADMINIDS\": $ADMINIDS/g" config.json
    sed -i "s/\"Cycletime\": [0-9]*/\"Cycletime\": $Cycletime/g" config.json
    sed -i "s/\"Debug\": \(true\|false\)/\"Debug\": $Debug/g" config.json
    sed -i "s#\"ProxyURL\": \".*\"#\"ProxyURL\": \"$ProxyURL\"#g" config.json
    sed -i "s#\"Pushinfo\": \".*\"#\"Pushinfo\": \"$Pushinfo\"#g" config.json
    ./TGBot_RSS > /dev/null 2>&1 &
fi

tail -f /dev/null