#!/bin/bash

# 从环境变量文件中读取变量
source ./common_vars.env

# 如果有参数传入，则只加载指定的镜像，否则加载全部镜像
if [ "$#" -eq 0 ]; then
    images_to_load=("${DOCKER_IMAGES[@]}")
else
    images_to_load=("$@")
fi
#
## 解压
#echo "Unzipping $ZIP_FILE"
#unzip -o "$ZIP_FILE" -d "$DESTINATION/"
#进入解压后的目录
#echo "cd $DESTINATION/up"
#cd "$DESTINATION/up" || exit
# 输出当前工作目录，检查是否成功切换到解压后的目录
echo "Current directory: $(pwd)"
# 修改每个YAML文件中的etcd-conf字段
for yaml_file in "${YAML_FILES[@]}"; do
    etcd_addresses=""
    etcd_addresses=${IP_LIST[@]}
    echo "Updating YAML file $yaml_file"
    sed -i '/^ *etcd-conf: *$/,/^[^ ]/ s/^ *address: .*/  address: '\"$etcd_addresses\"'/' "$yaml_file"
done

# 在本地加载镜像
for image in "${images_to_load[@]}"; do
    echo "Loading image $image"
    docker load -i "$image"
    # 检查docker load命令的退出状态
    if [ $? -ne 0 ]; then
        echo "Error loading image $image. Exiting..."
        exit 1
    fi
done

# 在本地使用docker-compose启动容器
echo "Starting containers"
docker-compose -f "$DOCKER_COMPOSE_FILE" up -d

