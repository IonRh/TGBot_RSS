#!/bin/bash
export TZ='Asia/Shanghai'

if [ -f /root/config.json ]; then
    cp -f /app/TGBot_RSS /root/TGBot_RSS
    cd /root
    ./TGBot_RSS
else
    mv /app/config.json /root/ || exit 1
    cp -f /app/TGBot_RSS /root/TGBot_RSS
    cd /root

    # 建议加引号防止变量为空或含特殊字符
    sed -i "s/\"BotToken\": \".*\"/\"BotToken\": \"$BotToken\"/g" config.json
    sed -i "s/\"ADMINIDS\": [0-9]*/\"ADMINIDS\": $ADMINIDS/g" config.json
    sed -i "s/\"Cycletime\": [0-9]*/\"Cycletime\": $Cycletime/g" config.json
    sed -i "s/\"Debug\": \(true\|false\)/\"Debug\": $Debug/g" config.json
    sed -i "s#\"ProxyURL\": \".*\"#\"ProxyURL\": \"$ProxyURL\"#g" config.json
    sed -i "s#\"Pushinfo\": \".*\"#\"Pushinfo\": \"$Pushinfo\"#g" config.json

    ./TGBot_RSS
fi
