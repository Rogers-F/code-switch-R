#!/bin/bash

# Ensure ~/go/bin is in PATH so wails3 can be found
export PATH="$PATH:$HOME/go/bin"

# Navigate to the script's directory to ensure we run in the project root
cd "$(dirname "$0")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Print header
echo -e "${CYAN}=================================================="
echo -e " 📦  Code Switch R Packaging & Build Helper"
echo -e "==================================================${NC}"

# Function to check if wails3 is installed
check_wails() {
  if ! command -v wails3 &> /dev/null; then
    echo -e "${RED}❌ Error: wails3 command not found.${NC}"
    echo -e "Please make sure Wails v3 is installed and in your PATH.${NC}"
    exit 1
  fi
}

# Function to run build task
run_task() {
  local task_name=$1
  local description=$2
  
  echo -e "\n${BLUE}⚙️  Running: ${description}...${NC}"
  wails3 task "$task_name"
  
  if [ $? -eq 0 ]; then
    echo -e "\n${GREEN}✨ Success! Completed ${description}.${NC}"
    echo -e "📁 Output files are in: ${YELLOW}$(pwd)/bin/${NC}"
  else
    echo -e "\n${RED}❌ Error: Failed to complete ${description}.${NC}"
    exit 1
  fi
}

# Parse command line arguments
if [ "$1" != "" ]; then
  check_wails
  case $1 in
    -c|--current)
      run_task "package" "Package for current architecture"
      exit 0
      ;;
    -u|--universal)
      run_task "package:universal" "Package Universal Bundle (arm64 + amd64)"
      exit 0
      ;;
    -b|--build)
      run_task "build" "Compile production binary"
      exit 0
      ;;
    -h|--help)
      echo "Usage:"
      echo "  ./build.sh                  - Show interactive selection menu"
      echo "  ./build.sh -c, --current    - Package for current architecture"
      echo "  ./build.sh -u, --universal  - Package Universal Bundle (arm64 + amd64)"
      echo "  ./build.sh -b, --build      - Compile production binary"
      echo "  ./build.sh -h, --help       - Show this help message"
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      echo "Run './build.sh --help' for usage."
      exit 1
      ;;
  esac
fi

# Interactive Mode
check_wails

echo -e "Please select a build option:"
echo -e "  ${GREEN}1)${NC} 🚀  打包当前架构的生产版本 (.app 包) - Recommended"
echo -e "  ${GREEN}2)${NC} 🌍  打包 Universal 通用版本 (arm64 + amd64)"
echo -e "  ${GREEN}3)${NC} 🛠️   仅编译生产版本可执行文件"
echo -e "  ${RED}4)${NC} ❌  退出"
echo

read -p "Enter choice [1-4]: " choice

case $choice in
  1)
    run_task "package" "Package for current architecture"
    ;;
  2)
    run_task "package:universal" "Package Universal Bundle (arm64 + amd64)"
    ;;
  3)
    run_task "build" "Compile production binary"
    ;;
  4)
    echo -e "${YELLOW}Cancelled.${NC}"
    exit 0
    ;;
  *)
    echo -e "${RED}Invalid option.${NC}"
    exit 1
    ;;
esac
