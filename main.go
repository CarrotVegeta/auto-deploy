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

	destPath, err := extractFilePathWithoutExt(zipFilePath)
	if err != nil {
		return err
	}
	if err := unzip(zipFilePath, destPath); err != nil {
		log.Fatal(err)
	}
	fmt.Println("ZIP file extracted successfully")
	fileName := filepath.Base(zipFilePath)
	remotePath := "./" + strings.TrimSuffix(fileName, ".zip")
	_ = client.Mkdir(remotePath)
	err = uploadDirectory(client, destPath, remotePath)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("upload file to remote server")
	// 复制zip文件到远程服务器
	envName := filepath.Base(envPath)
	err = copyFile(client, envPath, remotePath+"/"+envName)
	if err != nil {
		return fmt.Errorf("failed to copy env file: %v", err)
	}
	fmt.Println("execute sh")
	// 执行install.sh脚本
	installCommand := fmt.Sprintf("cd %s &&  ./install.sh", remotePath)
	err = Run(conn, installCommand)
	if err != nil {
		return fmt.Errorf("failed to execute command on server: %v", err)
	}
	// 执行install.sh脚本
	//err = deleteDir(destPath)
	//if err != nil {
	//	return fmt.Errorf("failed to remove local unzip file: %v", err)
	//}
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

// 删除文件夹及其所有内容
func deleteDir(dir string) error {
	// 遍历目录
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // 如果在遍历过程中遇到错误，返回该错误
		}
		return os.RemoveAll(path) // 删除文件或目录
	})
	if err != nil {
		return err
	}
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

// 解压ZIP文件到指定目录
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	os.MkdirAll(dest, 0755)

	for _, f := range r.File {
		filePath := filepath.Join(dest, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(filePath, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)

		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

// 上传目录内容
func uploadDirectory(client *sftp.Client, localPath, remotePath string) error {
	localFiles, err := os.ReadDir(localPath)
	if err != nil {
		return err
	}
	for _, file := range localFiles {
		localFilePath := filepath.Join(localPath, file.Name())
		remoteFilePath := filepath.Join(remotePath, file.Name())

		if file.IsDir() {
			_ = client.Mkdir(remoteFilePath)
			//if err != nil {
			//	return err
			//}
			err = uploadDirectory(client, localFilePath, remoteFilePath)
			if err != nil {
				return err
			}
		} else {
			err := uploadFile(client, localFilePath, remoteFilePath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// 上传单个文件
func uploadFile(client *sftp.Client, localFilePath, remoteFilePath string) error {
	localFile, err := os.Open(localFilePath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	remoteFile, err := client.Create(remoteFilePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()
	// 设置文件权限为755
	if err := remoteFile.Chmod(0755); err != nil {
		log.Fatal("Failed to set file permissions: ", err)
	}
	_, err = io.Copy(remoteFile, localFile)
	return err
}

// extractFilePathWithoutExt 提取给定ZIP文件路径，并返回去掉.zip后缀的路径
func extractFilePathWithoutExt(zipFilePath string) (string, error) {
	// 提取文件的后缀
	ext := filepath.Ext(zipFilePath)
	if ext != ".zip" {
		return "", fmt.Errorf("the file is not a ZIP file: %s", zipFilePath)
	}

	// 去掉.zip后缀
	withoutExt := strings.TrimSuffix(zipFilePath, ext)
	return withoutExt, nil
}
