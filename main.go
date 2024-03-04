package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/joho/godotenv"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	if len(os.Args) == 1 {
		// 检查是否至少有一个参数
		fmt.Println("Usage: program_name env_path")
		return
	}
	envPath := os.Args[1]
	// 加载.env文件
	err := godotenv.Load(os.Args[1])
	if err != nil {
		log.Fatalf("Error loading %s file: %v", envPath, err)
	}

	// 从环境变量中读取用户名、密码、服务器地址和zip文件路径
	username := os.Getenv("USERNAME")
	password := os.Getenv("PASSWORD")
	serverAddresses := os.Getenv("SERVER_ADDRESS")
	zipFilePath := os.Getenv("ZIP_FILE_PATH")

	// 检查环境变量是否存在
	if username == "" || password == "" || serverAddresses == "" || zipFilePath == "" {
		fmt.Println("Missing environment variable(s). Please make sure USERNAME, PASSWORD, SERVER_ADDRESS, and ZIP_FILE_PATH are set.")
		return
	}

	// 将逗号分隔的服务器地址拆分成切片
	servers := strings.Split(serverAddresses, ",")

	// 并发执行派发任务
	var wg sync.WaitGroup
	for _, server := range servers {
		wg.Add(1)
		go func(server string) {
			defer wg.Done()
			err := dispatchAndExecute(server, username, password, zipFilePath, envPath)
			if err != nil {
				fmt.Printf("Failed to dispatch and execute on server %s: %v\n", server, err)
				return
			}
			fmt.Printf("Dispatch and execute on server %s completed\n", server)
		}(server)
	}
	wg.Wait()
}

// dispatchAndExecute 函数负责在指定服务器上执行派发和执行命令的操作
func dispatchAndExecute(server, username, password, zipFilePath, envPath string) error {
	// SSH连接配置
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// 建立SSH连接
	conn, err := ssh.Dial("tcp", server, config)
	if err != nil {
		return fmt.Errorf("failed to dial server: %v", err)
	}
	defer conn.Close()

	// 创建SCP客户端
	client, err := sftp.NewClient(conn)
	if err != nil {
		return fmt.Errorf("failed to create SCP client: %v", err)
	}
	defer client.Close()

	// 复制zip文件到远程服务器
	err = copyFile(client, zipFilePath, "./up.zip")
	if err != nil {
		return fmt.Errorf("failed to copy zip file: %v", err)
	}
	// 执行unzip命令
	unzipCmd := fmt.Sprintf("unzip -o %s ", "./up.zip")
	err = Run(conn, unzipCmd)
	if err != nil {
		return fmt.Errorf("failed to unzip: %v", err)
	}
	// 复制zip文件到远程服务器
	envName := filepath.Base(envPath)
	err = copyFile(client, envPath, "./up/"+envName)
	if err != nil {
		return fmt.Errorf("failed to copy env file: %v", err)
	}
	// 执行install.sh脚本
	err = Run(conn, "cd ~/up &&  ./install.sh")
	if err != nil {
		return fmt.Errorf("failed to execute command on server: %v", err)
	}

	return nil
}
func Run(conn *ssh.Client, command string) error {
	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		fmt.Println(string(output))
		return fmt.Errorf("failed to execute command on server: %v", err)
	}
	fmt.Println(string(output))
	return nil
}

// copyFile 用于复制文件到远程服务器
func copyFile(client *sftp.Client, srcPath, destPath string) error {
	// 打开本地文件
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer srcFile.Close()

	// 创建远程文件
	destFile, err := client.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %v", err)
	}
	defer destFile.Close()

	// 复制文件内容
	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file content: %v", err)
	}

	return nil
}

// unzip 函数用于解压zip包
func unzip(zipFilePath string) error {
	// 第一步，打开 zip 文件
	zipFile, err := zip.OpenReader(zipFilePath)
	if err != nil {
		return fmt.Errorf("error opening zip file:%v", err)
	}
	defer zipFile.Close()

	// 第二步，遍历 zip 中的文件
	for _, f := range zipFile.File {
		filePath := f.Name
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(filePath, os.ModePerm)
			continue
		}
		fmt.Println(filepath.Dir(filePath))
		dirPath := "./up"
		// 创建对应文件夹
		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			return fmt.Errorf("error create unzio dir:%v", err)
		}
		// 解压到的目标文件
		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to open zip file: %v", err)
		}
		file, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open zipped file: %v", err)
		}
		// 写入到解压到的目标文件
		if _, err := io.Copy(dstFile, file); err != nil {
			return fmt.Errorf("failed to copy zipped file content: %v", err)
		}
		dstFile.Close()
		file.Close()
	}
	return nil
}
