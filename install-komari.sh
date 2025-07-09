#!/bin/bash

# Color definitions for terminal output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "$1"
}

log_success() {
    echo -e "${GREEN}$1${NC}"
}

log_error() {
    echo -e "${RED}$1${NC}"
}

log_step() {
    echo -e "${YELLOW}$1${NC}"
}

# Global variables
INSTALL_DIR="/opt/komari"
DATA_DIR="/opt/komari/data"
SERVICE_NAME="komari"
BINARY_PATH="$INSTALL_DIR/komari"
LOG_LEVEL="info"  # 默认日志级别

# Show help
show_help() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --log-level LEVEL    Set log level (info, debug) [default: info]"
    echo "  -h, --help          Show this help message"
    echo ""
    echo "Interactive mode will start if no options are provided."
    echo ""
    echo "Examples:"
    echo "  $0                     # 交互式安装"
    echo "  $0 --log-level debug  # 安装并设置为DEBUG日志级别"
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --log-level)
                LOG_LEVEL="$2"
                if [[ "$LOG_LEVEL" != "info" && "$LOG_LEVEL" != "debug" ]]; then
                    log_error "无效的日志级别: $LOG_LEVEL (支持: info, debug)"
                    exit 1
                fi
                shift 2
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

# Show banner
show_banner() {
    clear
    echo "=============================================================="
    echo "            Komari Monitoring System Installer"
    echo "       https://github.com/lizkes/komari"
    echo "=============================================================="
    echo
    if [[ "$LOG_LEVEL" == "debug" ]]; then
        echo "日志级别: DEBUG (详细诊断模式)"
    else
        echo "日志级别: INFO (标准模式)"
    fi
    echo
}

# Check if running as root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "请使用 root 权限运行此脚本"
        exit 1
    fi
}

# Check for systemd
check_systemd() {
    if ! command -v systemctl >/dev/null 2>&1; then
        return 1
    else
        return 0
    fi
}

# Detect system architecture
detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "amd64"
            ;;
        aarch64)
            echo "arm64"
            ;;
        i386|i686)
            echo "386"
            ;;
        riscv64)
            echo "riscv64"
            ;;
        *)
            log_error "不支持的架构: $arch"
            exit 1
            ;;
    esac
}

# Check if Komari is already installed
is_installed() {
    if [ -f "$BINARY_PATH" ]; then
        return 0 # 0 means true in bash exit codes
    else
        return 1 # 1 means false
    fi
}

# Install dependencies
install_dependencies() {
    log_step "检查并安装依赖..."

    if ! command -v curl >/dev/null 2>&1; then
        if command -v apt >/dev/null 2>&1; then
            log_info "使用 apt 安装依赖..."
            apt update
            apt install -y curl
        elif command -v yum >/dev/null 2>&1; then
            log_info "使用 yum 安装依赖..."
            yum install -y curl
        elif command -v apk >/dev/null 2>&1; then
            log_info "使用 apk 安装依赖..."
            apk add curl
        else
            log_error "未找到支持的包管理器 (apt/yum/apk)"
            exit 1
        fi
    fi
}

# Binary installation
install_binary() {
    log_step "开始二进制安装..."

    if is_installed; then
        log_info "Komari 已安装。要升级，请使用升级选项。"
        return
    fi

    install_dependencies

    local arch=$(detect_arch)
    log_info "检测到架构: $arch"

    log_step "创建安装目录: $INSTALL_DIR"
    mkdir -p "$INSTALL_DIR"

    log_step "创建数据目录: $DATA_DIR"
    mkdir -p "$DATA_DIR"

    local file_name="komari-linux-${arch}"
    local download_url="https://github.com/lizkes/komari/releases/latest/download/${file_name}"

    log_step "下载 Komari 二进制文件..."
    log_info "URL: $download_url"

    if ! curl -L -o "$BINARY_PATH" "$download_url"; then
        log_error "下载失败"
        return 1
    fi

    chmod +x "$BINARY_PATH"
    log_success "Komari 二进制文件安装完成: $BINARY_PATH"

    if ! check_systemd; then
        log_step "警告：未检测到 systemd，跳过服务创建。"
        log_step "您可以从命令行手动运行 Komari："
        log_step "    $BINARY_PATH server -l 0.0.0.0:25774"
        echo
        log_success "安装完成！"
        return
    fi

    create_systemd_service

    systemctl daemon-reload
    systemctl enable ${SERVICE_NAME}.service
    systemctl start ${SERVICE_NAME}.service

    if systemctl is-active --quiet ${SERVICE_NAME}.service; then
        log_success "Komari 服务启动成功"
        
        log_step "正在获取初始密码..."
        sleep 5 
        local password=$(journalctl -u ${SERVICE_NAME} --since "1 minute ago" | grep "admin account created." | tail -n 1 | sed -e 's/.*admin account created.//')
        if [ -z "$password" ]; then
            log_error "未能获取初始密码，请检查日志"
        fi
        show_access_info "$password"
    else
        log_error "Komari 服务启动失败"
        log_info "查看日志: journalctl -u ${SERVICE_NAME} -f"
        return 1
    fi
}

# Create systemd service file
create_systemd_service() {
    log_step "创建 systemd 服务..."

    local service_file="/etc/systemd/system/${SERVICE_NAME}.service"
    local exec_start="${BINARY_PATH} server -l 0.0.0.0:25774"
    
    # 如果设置了非默认日志级别，添加到启动命令
    if [[ "$LOG_LEVEL" != "info" ]]; then
        exec_start="${exec_start} --log-level ${LOG_LEVEL}"
        log_info "服务将使用日志级别: $LOG_LEVEL"
    fi
    
    cat > "$service_file" << EOF
[Unit]
Description=Komari Monitor Service
After=network.target

[Service]
Type=simple
ExecStart=${exec_start}
WorkingDirectory=${DATA_DIR}
Restart=always
User=root
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

    log_success "systemd 服务文件创建完成"
    if [[ "$LOG_LEVEL" == "debug" ]]; then
        log_info "服务启动命令: $exec_start"
    fi
}

# Show access information
show_access_info() {
    local password=$1
    echo
    log_success "安装完成！"
    echo
    log_info "访问信息："
    log_info "  URL: http://$(hostname -I | awk '{print $1}'):25774"
    if [ -n "$password" ]; then
        log_info "初始登录信息: $password"
    fi
    echo
    log_info "服务管理命令："
    log_info "  状态:  systemctl status $SERVICE_NAME"
    log_info "  启动:   systemctl start $SERVICE_NAME"
    log_info "  停止:    systemctl stop $SERVICE_NAME"
    log_info "  重启: systemctl restart $SERVICE_NAME"
    log_info "  日志:    journalctl -u $SERVICE_NAME -f"
    echo
    if [[ "$LOG_LEVEL" == "debug" ]]; then
        log_info "当前服务使用DEBUG日志级别，将输出详细的诊断信息"
        log_info "如需切换到INFO级别，请编辑: /etc/systemd/system/${SERVICE_NAME}.service"
        log_info "然后执行: systemctl daemon-reload && systemctl restart $SERVICE_NAME"
    fi
}

# Upgrade function
upgrade_komari() {
    log_step "升级 Komari..."

    if ! is_installed; then
        log_error "Komari 未安装。请先安装它。"
        return 1
    fi

    if ! check_systemd; then
        log_error "未检测到 systemd。无法管理服务。"
        return 1
    fi

    log_step "停止 Komari 服务..."
    systemctl stop ${SERVICE_NAME}.service

    log_step "备份当前二进制文件..."
    cp "$BINARY_PATH" "${BINARY_PATH}.backup.$(date +%Y%m%d_%H%M%S)"

    local arch=$(detect_arch)
    local file_name="komari-linux-${arch}"
    local download_url="https://github.com/lizkes/komari/releases/latest/download/${file_name}"

    log_step "下载最新版本..."
    if ! curl -L -o "$BINARY_PATH" "$download_url"; then
        log_error "下载失败，正在从备份恢复"
        mv "${BINARY_PATH}.backup."* "$BINARY_PATH"
        systemctl start ${SERVICE_NAME}.service
        return 1
    fi

    chmod +x "$BINARY_PATH"

    log_step "重启 Komari 服务..."
    systemctl start ${SERVICE_NAME}.service

    if systemctl is-active --quiet ${SERVICE_NAME}.service; then
        log_success "Komari 升级成功"
    else
        log_error "服务在升级后未能启动"
    fi
}

# Uninstall function
uninstall_komari() {
    log_step "卸载 Komari..."

    if ! is_installed; then
        log_info "Komari 未安装"
        return 0
    fi

    read -p "这将删除 Komari。您确定吗？(Y/n): " confirm
    if [[ $confirm =~ ^[Nn]$ ]]; then
        log_info "卸载已取消"
        return 0
    fi

    if check_systemd; then
        log_step "停止并禁用服务..."
        systemctl stop ${SERVICE_NAME}.service >/dev/null 2>&1
        systemctl disable ${SERVICE_NAME}.service >/dev/null 2>&1
        rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
        systemctl daemon-reload
        log_success "systemd 服务已删除"
    fi

    log_step "删除二进制文件..."
    rm -f "$BINARY_PATH"
    # 尝试在目录为空时删除该目录
    rmdir "$INSTALL_DIR" 2>/dev/null || log_info "数据目录 $INSTALL_DIR 不为空，未删除"
    log_success "Komari 二进制文件已删除"

    log_success "Komari 卸载完成"
    log_info "数据文件保留在 $DATA_DIR"
}

# Show service status
show_status() {
    if ! is_installed; then
        log_error "Komari 未安装"
        return
    fi
    if ! check_systemd; then
        log_error "未检测到 systemd。无法获取服务状态。"
        return
    fi
    log_step "Komari 服务状态:"
    systemctl status ${SERVICE_NAME}.service --no-pager -l
}

# Show service logs
show_logs() {
    if ! is_installed; then
        log_error "Komari 未安装"
        return
    fi
    if ! check_systemd; then
        log_error "未检测到 systemd。无法获取服务日志。"
        return
    fi
    log_step "查看 Komari 服务日志..."
    journalctl -u ${SERVICE_NAME} -f --no-pager
}

# Restart service
restart_service() {
    if ! is_installed; then
        log_error "Komari 未安装"
        return
    fi
    if ! check_systemd; then
        log_error "未检测到 systemd。无法重启服务。"
        return
    fi
    log_step "重启 Komari 服务..."
    systemctl restart ${SERVICE_NAME}.service
    if systemctl is-active --quiet ${SERVICE_NAME}.service; then
        log_success "服务重启成功"
    else
        log_error "服务重启失败"
    fi
}

# Stop service
stop_service() {
    if ! is_installed; then
        log_error "Komari 未安装"
        return
    fi
    if ! check_systemd; then
        log_error "未检测到 systemd。无法停止服务。"
        return
    fi
    log_step "停止 Komari 服务..."
    systemctl stop ${SERVICE_NAME}.service
    log_success "服务已停止"
}

# Change log level
change_log_level() {
    if ! is_installed; then
        log_error "Komari 未安装"
        return
    fi
    if ! check_systemd; then
        log_error "未检测到 systemd。无法管理服务。"
        return
    fi
    
    echo "当前日志级别配置："
    local current_level=$(grep "ExecStart" "/etc/systemd/system/${SERVICE_NAME}.service" | grep -o "log-level [a-z]*" | cut -d' ' -f2)
    if [[ -z "$current_level" ]]; then
        current_level="info"
    fi
    log_info "  当前级别: $current_level"
    echo
    echo "选择新的日志级别："
    echo "  1) info  - 标准日志输出"
    echo "  2) debug - 详细诊断日志 (用于故障排查)"
    echo "  3) 取消"
    echo
    
    read -p "输入选项 [1-3]: " level_choice
    
    case $level_choice in
        1)
            new_level="info"
            ;;
        2)
            new_level="debug"
            ;;
        3)
            log_info "已取消"
            return
            ;;
        *)
            log_error "无效选项"
            return
            ;;
    esac
    
    if [[ "$new_level" == "$current_level" ]]; then
        log_info "日志级别已经是 $new_level，无需更改"
        return
    fi
    
    log_step "更新日志级别为: $new_level"
    
    # 停止服务
    systemctl stop ${SERVICE_NAME}.service
    
    # 更新服务文件
    LOG_LEVEL="$new_level"
    create_systemd_service
    
    # 重新加载并启动服务
    systemctl daemon-reload
    systemctl start ${SERVICE_NAME}.service
    
    if systemctl is-active --quiet ${SERVICE_NAME}.service; then
        log_success "日志级别已更新为: $new_level"
        if [[ "$new_level" == "debug" ]]; then
            log_info "现在将输出详细的网络和连接诊断信息"
            log_info "查看实时日志: journalctl -u $SERVICE_NAME -f"
        fi
    else
        log_error "服务启动失败，请检查日志"
    fi
}

# Main menu
main_menu() {
    show_banner
    echo "请选择操作："
    echo "  1) 安装 Komari"
    echo "  2) 升级 Komari"
    echo "  3) 卸载 Komari"
    echo "  4) 查看状态"
    echo "  5) 查看日志"
    echo "  6) 重启服务"
    echo "  7) 停止服务"
    echo "  8) 切换日志级别"
    echo "  9) 退出"
    echo

    read -p "输入选项 [1-9]: " choice

    case $choice in
        1) install_binary ;;
        2) upgrade_komari ;;
        3) uninstall_komari ;;
        4) show_status ;;
        5) show_logs ;;
        6) restart_service ;;
        7) stop_service ;;
        8) change_log_level ;;
        9) exit 0 ;;
        *) log_error "无效选项" ;;
    esac
}

# Main execution
parse_args "$@"
check_root

# 如果没有参数，显示交互菜单，否则执行安装
if [[ $# -eq 0 ]]; then
    main_menu
else
    show_banner
    install_binary
fi
