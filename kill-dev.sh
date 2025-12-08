#!/bin/bash

# --- 核心配置 ---
# 定义清理函数
cleanup() {
    [ -f "$tmp_file" ] && rm -f "$tmp_file"
}
trap cleanup EXIT

echo "=== Wails v3 强力清道夫 (适配 Code Switch) ==="
echo "正在扫描 Wails3 及其衍生的 Node 和 Binary 进程..."
echo ""

# 创建临时数组
pids=()
commands=()
paths=() 
index=1

tmp_file=$(mktemp)
# 搜索 wails3 进程 (适配 v3 命令)
ps -awf | grep -i "wails" | grep -v grep | grep -v $$ > "$tmp_file"

# --- 核心逻辑函数：三维打击 ---
kill_project_tree() {
    local target_pid=$1
    local target_path=$2
    
    echo "---------------------------------------"
    # 1. 杀掉 Wails3 管家进程
    if [ -n "$target_pid" ]; then
        echo "🔧 处理 Wails CLI (PID: $target_pid)..."
        kill -9 "$target_pid" 2>/dev/null
    fi

    # 2. 只有当成功获取到路径，且路径安全时才执行深度清理
    if [ -n "$target_path" ] && [ "$target_path" != "/" ]; then
        echo "📂 扫描项目目录: $target_path"
        
        # 核心修改：同时查找 node 和 bin 目录下的可执行文件
        # egrep "node|/bin/" 会匹配：
        # 1. 所有的 node 进程
        # 2. 位于 .../code-switch-R/bin/ 下的 Code Switch 二进制文件
        
        related_pids=$(lsof +D "$target_path" 2>/dev/null | awk '$1=="node" || $9 ~ /\/bin\// {print $2}' | sort -u)
        
        if [ -n "$related_pids" ]; then
            # 把 PID 换行转为空格，方便遍历
            for npid in $related_pids; do
                # 获取进程名方便展示
                pname=$(ps -p "$npid" -o comm= 2>/dev/null | awk -F/ '{print $NF}')
                if [ -n "$pname" ]; then
                    kill -9 "$npid" 2>/dev/null && echo "   -> 💀 已击杀: $pname (PID: $npid)"
                fi
            done
        else
            echo "   ✨ 目录下无残留进程 (干净)"
        fi
    else
        echo "⚠️  无法获取项目路径，跳过深度清理"
    fi
}

# --- 读取进程信息 ---
while IFS= read -r line; do
    if [[ -n "$line" ]]; then
        pid=$(echo "$line" | awk '{print $2}')
        # 提取更完整的命令
        cmd=$(echo "$line" | cut -d' ' -f11-)
        
        # 获取工作目录 (Project Path)
        work_dir=$(lsof -p "$pid" 2>/dev/null | grep "cwd" | awk '{print $NF}' | head -n 1)
        
        if [ -n "$work_dir" ]; then
            pids[index]=$pid
            commands[index]=$cmd
            paths[index]=$work_dir
            index=$((index + 1))
        fi
    fi
done < "$tmp_file"

# --- 显示列表 ---
echo "发现以下开发会话："
echo "------------------------------------------------------------------------"
printf "%-4s | %-7s | %-30s\n" "No." "PID" "项目位置"
echo "------------------------------------------------------------------------"

for ((i=1; i<index; i++)); do
    short_path=$(echo "${paths[i]}" | awk -F/ '{print $(NF-1)"/"$NF}')
    printf "%-4s | %-7s | .../%-26s\n" "$i" "${pids[i]}" "$short_path"
done
echo "------------------------------------------------------------------------"

if [ $index -eq 1 ]; then
    echo "✅ 没有找到运行中的 Wails 进程。"
    exit 0
fi

# --- 交互操作 ---
echo ""
echo "选项: [数字] 单杀 / [all] 全杀 / [q] 退出"
read -e -p "请输入: " selection

case "$selection" in
    [qQ]*)
        echo "已取消"
        exit 0
        ;;
    [aA]ll|[aA])
        read -e -p "⚠️  确定清理所有环境吗？(y/n): " confirm
        if [[ $confirm == [Yy]* ]]; then
            for ((i=1; i<index; i++)); do
                kill_project_tree "${pids[i]}" "${paths[i]}"
            done
            echo "✅ 全部清理完成！"
        fi
        ;;
    *)
        # 简单的单选/多选处理逻辑
        selection=${selection//,/ }
        read -ra input_indices <<< "$selection"
        
        for idx in "${input_indices[@]}"; do
             if [ -n "${pids[idx]}" ]; then
                 kill_project_tree "${pids[idx]}" "${paths[idx]}"
             fi
        done
        echo "✅ 操作完成"
        ;;
esac