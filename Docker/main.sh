#!/bin/bash

export TZ='Asia/Shanghai'
# 检查/root是否有文件
if [ "$(ls -A /root/)" ]; then
	# 有文件则执行TGBot_RSS
	cd /root && ./TGBot_RSS
else
	# 无文件则移动/app/到/root
	mv /app/config.json /root/
    mv /app/TGBot_RSS /root/
    echo "请按需求修改 config.json 配置文件"
    echo "Modify the config.json configuration file as needed."
fi

tail -f /dev/null