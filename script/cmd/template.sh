#!/bin/sh
{{.DoNotEdit}}

NZ_BASE_PATH="/opt/nezha"
NZ_DASHBOARD_PATH="${NZ_BASE_PATH}/dashboard"
NZ_AGENT_PATH="${NZ_BASE_PATH}/agent"
NZ_DASHBOARD_SERVICE="/etc/systemd/system/nezha-dashboard.service"
NZ_DASHBOARD_SERVICERC="/etc/init.d/nezha-dashboard"

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'
export PATH="$PATH:/usr/local/bin"

os_arch=""
[ -e /etc/os-release ] && grep -i "PRETTY_NAME" /etc/os-release | grep -qi "alpine" && os_alpine='1'

sudo() {
    myEUID=$(id -ru)
    if [ "$myEUID" -ne 0 ]; then
        if command -v sudo > /dev/null 2>&1; then
            command sudo "$@"
        else
            err "{{.SudoError}}"
            exit 1
        fi
    else
        "$@"
    fi
}

check_systemd() {
    if [ "$os_alpine" != 1 ] && ! command -v systemctl >/dev/null 2>&1; then
        echo "{{.SystemdCheckError}}"
        exit 1
    fi
}

err() {
    printf "${red}%s${plain}\n" "$*" >&2
}

success() {
    printf "${green}%s${plain}\n" "$*"
}

info() {
    printf "${yellow}%s${plain}\n" "$*"
}

geo_check() {
    api_list="https://blog.cloudflare.com/cdn-cgi/trace https://dash.cloudflare.com/cdn-cgi/trace https://developers.cloudflare.com/cdn-cgi/trace"
    ua="Mozilla/5.0 (X11; Linux x86_64; rv:60.0) Gecko/20100101 Firefox/81.0"
    set -- "$api_list"
    for url in $api_list; do
        text="$(curl -A "$ua" -m 10 -s "$url")"
        endpoint="$(echo "$text" | sed -n 's/.*h=\([^ ]*\).*/\1/p')"
        if echo "$text" | grep -qw 'CN'; then
            isCN=true
            break
        elif echo "$url" | grep -q "$endpoint"; then
            break
        fi
    done
}

pre_check() {
    umask 077

    ## os_arch
    if uname -m | grep -q 'x86_64'; then
        os_arch="amd64"
    elif uname -m | grep -q 'i386\|i686'; then
        os_arch="386"
    elif uname -m | grep -q 'aarch64\|armv8b\|armv8l'; then
        os_arch="arm64"
    elif uname -m | grep -q 'arm'; then
        os_arch="arm"
    elif uname -m | grep -q 's390x'; then
        os_arch="s390x"
    elif uname -m | grep -q 'riscv64'; then
        os_arch="riscv64"
    fi

    ## China_IP
    if [ -z "$CN" ]; then
        geo_check
        if [ -n "$isCN" ]; then
            echo "{{.GeoInfo}}"
            printf "{{.PromptMirror}}"
            read -r input
            case $input in
            [yY][eE][sS] | [yY])
                echo "{{.UseChineseMirror}}"
                CN=true
                ;;

            [nN][oO] | [nN])
                echo "{{.NoUseChineseMirror}}"
                ;;

            [3])
                echo "{{.UseCustomMirror}}"
                printf "{{.PromptCustomImage}}"
                read -r input
                case $input in
                *)
                    CUSTOM_MIRROR=$input
                    ;;
                esac
                ;;
            *)
                echo "{{.NoUseChineseMirror}}"
                ;;
            esac
        fi
    fi

    if [ -n "$CUSTOM_MIRROR" ]; then
        GITHUB_RAW_URL="gitee.com/naibahq/scripts/raw/v0"
        GITHUB_URL=$CUSTOM_MIRROR
        Get_Docker_URL="get.docker.com"
        Get_Docker_Argu=" -s docker --mirror Aliyun"
        Docker_IMG="registry.cn-shanghai.aliyuncs.com\/naibahq\/nezha-dashboard:v0.20.13"
    else
        if [ -z "$CN" ]; then
            GITHUB_RAW_URL="raw.githubusercontent.com/nezhahq/scripts/v0"
            GITHUB_URL="github.com"
            Get_Docker_URL="get.docker.com"
            Get_Docker_Argu=" "
            Docker_IMG="ghcr.io\/naiba\/nezha-dashboard:v0.20.13"
        else
            GITHUB_RAW_URL="gitee.com/naibahq/scripts/raw/v0"
            GITHUB_URL="gitee.com"
            Get_Docker_URL="get.docker.com"
            Get_Docker_Argu=" -s docker --mirror Aliyun"
            Docker_IMG="registry.cn-shanghai.aliyuncs.com\/naibahq\/nezha-dashboard:v0.20.13"
        fi
    fi
}

installation_check() {
    if docker compose version >/dev/null 2>&1; then
        DOCKER_COMPOSE_COMMAND="docker compose"
        if sudo $DOCKER_COMPOSE_COMMAND ls | grep -qw "$NZ_DASHBOARD_PATH/docker-compose.yaml" >/dev/null 2>&1; then
            NEZHA_IMAGES=$(sudo docker images --format "{{"{{.Repository}}"}}:{{"{{.Tag}}"}}" | grep -w "nezha-dashboard")
            if [ -n "$NEZHA_IMAGES" ]; then
                echo "{{.DockerImageFound}}"
                echo "$NEZHA_IMAGES"
                IS_DOCKER_NEZHA=1
                FRESH_INSTALL=0
                return
            else
                echo "{{.DockerImageNotFound}}"
            fi
        fi
    elif command -v docker-compose >/dev/null 2>&1; then
        DOCKER_COMPOSE_COMMAND="docker-compose"
        if sudo $DOCKER_COMPOSE_COMMAND -f "$NZ_DASHBOARD_PATH/docker-compose.yaml" config >/dev/null 2>&1; then
            NEZHA_IMAGES=$(sudo docker images --format "{{"{{.Repository}}"}}:{{"{{.Tag}}"}}" | grep -w "nezha-dashboard")
            if [ -n "$NEZHA_IMAGES" ]; then
                echo "{{.DockerImageFound}}"
                echo "$NEZHA_IMAGES"
                IS_DOCKER_NEZHA=1
                FRESH_INSTALL=0
                return
            else
                echo "{{.DockerImageNotFound}}"
            fi
        fi
    fi

    if [ -f "$NZ_DASHBOARD_PATH/app" ]; then
        IS_DOCKER_NEZHA=0
        FRESH_INSTALL=0
    fi
}

select_version() {
    if [ -z "$IS_DOCKER_NEZHA" ]; then
        info "{{.SelectInstallMethod}}"
        info "1. Docker"
        info "2. {{.StandaloneInstall}}"
        while true; do
            printf "{{.PromptSelectVersion}}"
            read -r option
            case "${option}" in
                1)
                    IS_DOCKER_NEZHA=1
                    break
                    ;;
                2)
                    IS_DOCKER_NEZHA=0
                    break
                    ;;
                *)
                    err "{{.ErrorSelectVersion}}"
                    ;;
            esac
        done
    fi
}

update_script() {
    echo "> {{.UpdateScript}}"

    curl -sL https://${GITHUB_RAW_URL}/{{.script}} -o /tmp/nezha.sh
    mv -f /tmp/nezha.sh ./nezha.sh && chmod a+x ./nezha.sh

    echo "{{.ExecuteScriptInfo}}"
    sleep 3s
    clear
    exec ./nezha.sh
    exit 0
}

before_show_menu() {
    echo && info "* {{.BeforeShowMenu}} *" && read temp
    show_menu
}

install_base() {
    (command -v curl >/dev/null 2>&1 && command -v wget >/dev/null 2>&1 && command -v unzip >/dev/null 2>&1 && command -v getenforce >/dev/null 2>&1) ||
        (install_soft curl wget unzip)
}

install_arch() {
    info "{{.InstallArchPrompt1}}"
    read -r -p "{{.InstallArchPrompt2}}" input
    case $input in
    [yY][eE][sS] | [yY])
        useradd -m nezha-agent
        sed -i "$ a\nezha-agent ALL=(ALL ) NOPASSWD:ALL" /etc/sudoers
        sudo -iu nezha-agent bash -c 'gpg --keyserver keys.gnupg.net --recv-keys 4695881C254508D1;
                                        cd /tmp; git clone https://aur.archlinux.org/libsepol.git; cd libsepol; makepkg -si --noconfirm --asdeps; cd ..;
                                        git clone https://aur.archlinux.org/libselinux.git; cd libselinux; makepkg -si --noconfirm; cd ..;
                                        rm -rf libsepol libselinux'
        sed -i '/nezha-agent/d' /etc/sudoers && sleep 30s && killall -u nezha-agent && userdel -r nezha-agent
        info "{{.InstallArchPrompt3}}"
        ;;
    [nN][oO] | [nN])
        echo "{{.InstallArchPrompt4}}"
        ;;
    *)
        echo "{{.InstallArchPrompt4}}"
        exit 0
        ;;
    esac
}

install_soft() {
    (command -v yum >/dev/null 2>&1 && sudo yum makecache && sudo yum install "$@" selinux-policy -y) ||
        (command -v apt >/dev/null 2>&1 && sudo apt update && sudo apt install "$@" selinux-utils -y) ||
        (command -v pacman >/dev/null 2>&1 && sudo pacman -Syu "$@" base-devel --noconfirm && install_arch) ||
        (command -v apt-get >/dev/null 2>&1 && sudo apt-get update && sudo apt-get install "$@" selinux-utils -y) ||
        (command -v apk >/dev/null 2>&1 && sudo apk update && sudo apk add "$@" -f)
}

install_dashboard() {
    check_systemd
    install_base

    echo "> {{.InstallDashboard}}"

    # Nezha Monitoring Folder
    if [ ! "$FRESH_INSTALL" = 0 ]; then
        sudo mkdir -p $NZ_DASHBOARD_PATH
    else
        echo "{{.InstallDashboardInfo}}"
        printf "{{.PromptInstallDashboard}}"
        read -r input
        case $input in
        [yY][eE][sS] | [yY])
            echo "{{.ExitInstallation}}"
            exit 0
            ;;
        [nN][oO] | [nN])
            echo "{{.ContinueInstallation}}"
            ;;
        *)
            echo "{{.ExitInstallation}}"
            exit 0
            ;;
        esac
    fi

    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        install_dashboard_docker
    elif [ "$IS_DOCKER_NEZHA" = 0 ]; then
        install_dashboard_standalone
    fi

    modify_dashboard_config 0

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

install_dashboard_docker() {
    if [ ! "$FRESH_INSTALL" = 0 ]; then
        if ! command -v docker >/dev/null 2>&1; then
            echo "{{.InstallDockerInfo}}"
            if [ "$os_alpine" != 1 ]; then
                if ! curl -sL https://${Get_Docker_URL} | sudo bash -s "${Get_Docker_Argu}"; then
                    err "{{.ErrorFetchScript}} ${Get_Docker_URL}"
                    return 0
                fi
                sudo systemctl enable docker.service
                sudo systemctl start docker.service
            else
                sudo apk add docker docker-compose
                sudo rc-update add docker
                sudo rc-service docker start
            fi
            success "{{.InstallDockerSuccess}}"
            installation_check
        fi
    fi
}

install_dashboard_standalone() {
    if [ ! -d "${NZ_DASHBOARD_PATH}/resource/template/theme-custom" ] || [ ! -d "${NZ_DASHBOARD_PATH}/resource/static/custom" ]; then
        sudo mkdir -p "${NZ_DASHBOARD_PATH}/resource/template/theme-custom" "${NZ_DASHBOARD_PATH}/resource/static/custom" >/dev/null 2>&1
    fi
}

selinux() {
    #Check SELinux
    if command -v getenforce >/dev/null 2>&1; then
        if getenforce | grep '[Ee]nfor'; then
            echo "{{.SELinuxInfo}}"
            sudo setenforce 0 >/dev/null 2>&1
            find_key="SELINUX="
            sudo sed -ri "/^$find_key/c${find_key}disabled" /etc/selinux/config
        fi
    fi
}

install_agent() {
    install_base
    selinux

    echo "> {{.InstallAgent}}"

    # echo "{{.ObtainAgentVersion}}"


    # _version=$(curl -m 10 -sL "https://api.github.com/repos/nezhahq/agent/releases/latest" | grep "tag_name" | head -n 1 | awk -F ":" '{print $2}' | sed 's/\"//g;s/,//g;s/ //g')
    # if [ -z "$_version" ]; then
    #     _version=$(curl -m 10 -sL "https://gitee.com/api/v5/repos/naibahq/agent/releases/latest" | awk -F '"' '{for(i=1;i<=NF;i++){if($i=="tag_name"){print $(i+2)}}}')
    # fi
    # if [ -z "$_version" ]; then
    #     _version=$(curl -m 10 -sL "https://fastly.jsdelivr.net/gh/nezhahq/agent/" | grep "option\.value" | awk -F "'" '{print $2}' | sed 's/nezhahq\/agent@/v/g')
    # fi
    # if [ -z "$_version" ]; then
    #     _version=$(curl -m 10 -sL "https://gcore.jsdelivr.net/gh/nezhahq/agent/" | grep "option\.value" | awk -F "'" '{print $2}' | sed 's/nezhahq\/agent@/v/g')
    # fi

    # if [ -z "$_version" ]; then
    #     err "{{printf .ErrorObtainVersion "Agent"}} https://api.github.com/repos/nezhahq/agent/releases/latest"
    #     return 1
    # else
    #     echo "{{.CurrentVersionInfo}} ${_version}"
    # fi

    _version="v0.20.5"

    # Nezha Monitoring Folder
    sudo mkdir -p $NZ_AGENT_PATH

    echo "{{.DownloadAgent}}"
    if [ -z "$CN" ]; then
        NZ_AGENT_URL="https://${GITHUB_URL}/nezhahq/agent/releases/download/${_version}/nezha-agent_linux_${os_arch}.zip"
    else
        NZ_AGENT_URL="https://${GITHUB_URL}/naibahq/agent/releases/download/${_version}/nezha-agent_linux_${os_arch}.zip"
    fi

    _cmd="wget -t 2 -T 60 -O nezha-agent_linux_${os_arch}.zip $NZ_AGENT_URL >/dev/null 2>&1"
    if ! eval "$_cmd"; then
        err "{{.ErrorDownloadAgent}} ${GITHUB_URL}"
        return 1
    fi

    sudo unzip -qo nezha-agent_linux_${os_arch}.zip &&
        sudo mv nezha-agent $NZ_AGENT_PATH &&
        sudo rm -rf nezha-agent_linux_${os_arch}.zip README.md

    if [ $# -ge 3 ]; then
        modify_agent_config "$@"
    else
        modify_agent_config 0
    fi

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

modify_agent_config() {
    echo "> {{printf .ModifyConfiguration "Agent"}}"

    if [ $# -lt 3 ]; then
        echo "{{.ModifyAgentConfInfo}}"
            printf "{{.PromptModifyAgent_1}}"
            read -r nz_grpc_host
            printf "{{.PromptModifyAgent_2}}"
            read -r nz_grpc_port
            printf "{{.PromptModifyAgent_3}}"
            read -r nz_client_secret
            printf "{{.PromptModifyAgent_4}}"
            read -r nz_grpc_proxy
        echo "${nz_grpc_proxy}" | grep -qiw 'Y' && args='--tls'
        if [ -z "$nz_grpc_host" ] || [ -z "$nz_client_secret" ]; then
            err "{{.ErrorModifyConfig}}"
            before_show_menu
            return 1
        fi
        if [ -z "$nz_grpc_port" ]; then
            nz_grpc_port=5555
        fi
    else
        nz_grpc_host=$1
        nz_grpc_port=$2
        nz_client_secret=$3
        shift 3
        if [ $# -gt 0 ]; then
            args="$*"
        fi
    fi

    _cmd="sudo ${NZ_AGENT_PATH}/nezha-agent service install -s $nz_grpc_host:$nz_grpc_port -p $nz_client_secret $args >/dev/null 2>&1"

    if ! eval "$_cmd"; then
        sudo "${NZ_AGENT_PATH}"/nezha-agent service uninstall >/dev/null 2>&1
        sudo "${NZ_AGENT_PATH}"/nezha-agent service install -s "$nz_grpc_host:$nz_grpc_port" -p "$nz_client_secret" "$args" >/dev/null 2>&1
    fi
    
    success "{{printf .SuccessModifyConfig "Agent"}}"

    #if [[ $# == 0 ]]; then
    #    before_show_menu
    #fi
}

modify_dashboard_config() {
    echo "> {{printf .ModifyConfiguration "Dashboard"}}"

    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        if [ -n "$DOCKER_COMPOSE_COMMAND" ]; then
            echo "{{.DownloadDockerScript}}"
            _cmd="wget -t 2 -T 60 -O /tmp/nezha-docker-compose.yaml https://${GITHUB_RAW_URL}/extras/docker-compose.yaml >/dev/null 2>&1"
            if ! eval "$_cmd"; then
                err "{{.ErrorFetchScript}} ${GITHUB_RAW_URL}"
                return 0
            fi
        else
            err "{{.ErrorCheckDocker}} https://docs.docker.com/compose/install/linux/"
            before_show_menu
        fi
    fi

    _cmd="wget -t 2 -T 60 -O /tmp/nezha-config.yaml https://${GITHUB_RAW_URL}/extras/config.yaml >/dev/null 2>&1"
    if ! eval "$_cmd"; then
        err "{{.ErrorFetchScript}} ${GITHUB_RAW_URL}"
        return 0
    fi

    echo "{{.PromptModifyDashboard_1}}"
        echo "{{.PromptModifyDashboard_2}}"
        printf "{{.PromptModifyDashboard_3}}"
        read -r nz_oauth2_type
        printf "{{.PromptModifyDashboard_4}}"
        read -r nz_github_oauth_client_id
        printf "{{.PromptModifyDashboard_5}}"
        read -r nz_github_oauth_client_secret
        printf "{{.PromptModifyDashboard_6}}"
        read -r nz_admin_logins
        printf "{{.PromptModifyDashboard_7}}"
        read -r nz_site_title
        printf "{{.PromptModifyDashboard_8}}"
        read -r nz_site_port
        printf "{{.PromptModifyDashboard_9}}"
        read -r nz_grpc_port

    if [ -z "$nz_admin_logins" ] || [ -z "$nz_github_oauth_client_id" ] || [ -z "$nz_github_oauth_client_secret" ] || [ -z "$nz_site_title" ]; then
        err "{{.ErrorModifyConfig}}"
        before_show_menu
        return 1
    fi

    if [ -z "$nz_site_port" ]; then
        nz_site_port=8008
    fi
    if [ -z "$nz_grpc_port" ]; then
        nz_grpc_port=5555
    fi
    if [ -z "$nz_oauth2_type" ]; then
        nz_oauth2_type=github
    fi

    sed -i "s/nz_oauth2_type/${nz_oauth2_type}/" /tmp/nezha-config.yaml
    sed -i "s/nz_admin_logins/${nz_admin_logins}/" /tmp/nezha-config.yaml
    sed -i "s/nz_grpc_port/${nz_grpc_port}/" /tmp/nezha-config.yaml
    sed -i "s/nz_github_oauth_client_id/${nz_github_oauth_client_id}/" /tmp/nezha-config.yaml
    sed -i "s/nz_github_oauth_client_secret/${nz_github_oauth_client_secret}/" /tmp/nezha-config.yaml
    sed -i "s/nz_language/zh-CN/" /tmp/nezha-config.yaml
    sed -i "s/nz_site_title/${nz_site_title}/" /tmp/nezha-config.yaml
    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        sed -i "s/nz_site_port/${nz_site_port}/" /tmp/nezha-docker-compose.yaml
        sed -i "s/nz_grpc_port/${nz_grpc_port}/g" /tmp/nezha-docker-compose.yaml
        sed -i "s/nz_image_url/${Docker_IMG}/" /tmp/nezha-docker-compose.yaml
    elif [ "$IS_DOCKER_NEZHA" = 0 ]; then
        sed -i "s/80/${nz_site_port}/" /tmp/nezha-config.yaml
    fi

    sudo mkdir -p $NZ_DASHBOARD_PATH/data
    sudo mv -f /tmp/nezha-config.yaml ${NZ_DASHBOARD_PATH}/data/config.yaml
    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        sudo mv -f /tmp/nezha-docker-compose.yaml ${NZ_DASHBOARD_PATH}/docker-compose.yaml
    fi

    if [ "$IS_DOCKER_NEZHA" = 0 ]; then
        echo "{{.DownloadServiceScript}}"
        if [ "$os_alpine" != 1 ]; then
            _download="sudo wget -t 2 -T 60 -O $NZ_DASHBOARD_SERVICE https://${GITHUB_RAW_URL}/services/nezha-dashboard.service >/dev/null 2>&1"
            if ! eval "$_download"; then
                err "{{.ErrorFetchFile}} ${GITHUB_RAW_URL}"
                return 0
            fi
        else
            _download="sudo wget -t 2 -T 60 -O $NZ_DASHBOARD_SERVICERC https://${GITHUB_RAW_URL}/services/nezha-dashboard >/dev/null 2>&1"
            if ! eval "$_download"; then
                err "{{.ErrorFetchFile}} ${GITHUB_RAW_URL}"
                return 0
            fi
            sudo chmod +x $NZ_DASHBOARD_SERVICERC
        fi
    fi

    success "{{printf .SuccessModifyConfig "Dashboard"}}"

    restart_and_update

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

restart_and_update() {
    echo "> {{.RestartAndUpdate}}"

    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        _cmd="restart_and_update_docker"
    elif [ "$IS_DOCKER_NEZHA" = 0 ]; then
        _cmd="restart_and_update_standalone"
    fi

    if eval "$_cmd"; then
        success "{{.SuccessRestartDashboard}}"
        info "{{.DashboardInfo}}"
    else
        err "{{.ErrorRestartDashboard}}"
    fi

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

restart_and_update_docker() {
    update_docker_compose_image
    sudo $DOCKER_COMPOSE_COMMAND -f ${NZ_DASHBOARD_PATH}/docker-compose.yaml pull
    sudo $DOCKER_COMPOSE_COMMAND -f ${NZ_DASHBOARD_PATH}/docker-compose.yaml down
    sudo $DOCKER_COMPOSE_COMMAND -f ${NZ_DASHBOARD_PATH}/docker-compose.yaml up -d
}

update_docker_compose_image() {
    yaml_file_path="${NZ_DASHBOARD_PATH}/docker-compose.yaml"
    if grep -q "registry.cn-shanghai.aliyuncs.com/naibahq/nezha-dashboard$" "$yaml_file_path"; then
        sed -i 's|registry.cn-shanghai.aliyuncs.com/naibahq/nezha-dashboard$|registry.cn-shanghai.aliyuncs.com/naibahq/nezha-dashboard:v0.20.13|' "$yaml_file_path"
    fi
    if grep -q "ghcr.io/naiba/nezha-dashboard$" "$yaml_file_path"; then
        sed -i 's|ghcr.io/naiba/nezha-dashboard$|ghcr.io/naibahq/nezha-dashboard:v0.20.13|' "$yaml_file_path"
    fi
}

restart_and_update_standalone() {
    # _version=$(curl -m 10 -sL "https://api.github.com/repos/naiba/nezha/releases/latest" | grep "tag_name" | head -n 1 | awk -F ":" '{print $2}' | sed 's/\"//g;s/,//g;s/ //g')
    # if [ -z "$_version" ]; then
    #     _version=$(curl -m 10 -sL "https://fastly.jsdelivr.net/gh/naiba/nezha/" | grep "option\.value" | awk -F "'" '{print $2}' | sed 's/naiba\/nezha@/v/g')
    # fi
    # if [ -z "$_version" ]; then
    #     _version=$(curl -m 10 -sL "https://gcore.jsdelivr.net/gh/naiba/nezha/" | grep "option\.value" | awk -F "'" '{print $2}' | sed 's/naiba\/nezha@/v/g')
    # fi
    # if [ -z "$_version" ]; then
    #     _version=$(curl -m 10 -sL "https://gitee.com/api/v5/repos/naibahq/nezha/releases/latest" | awk -F '"' '{for(i=1;i<=NF;i++){if($i=="tag_name"){print $(i+2)}}}')
    # fi

    _version="v0.20.13"

    if [ -z "$_version" ]; then
        err "{{printf .ErrorObtainVersion "Dashboard"}} https://api.github.com/repos/naiba/nezha/releases/latest"
        return 1
    else
        echo "{{.CurrentVersionInfo}} ${_version}"
    fi

    if [ "$os_alpine" != 1 ]; then
        sudo systemctl daemon-reload
        sudo systemctl stop nezha-dashboard
    else
        sudo rc-service nezha-dashboard stop
    fi

    if [ -z "$CN" ]; then
        NZ_DASHBOARD_URL="https://${GITHUB_URL}/naiba/nezha/releases/download/${_version}/dashboard-linux-${os_arch}.zip"
    else
        NZ_DASHBOARD_URL="https://${GITHUB_URL}/naibahq/nezha/releases/download/${_version}/dashboard-linux-${os_arch}.zip"
    fi

    sudo wget -qO $NZ_DASHBOARD_PATH/app.zip "$NZ_DASHBOARD_URL" >/dev/null 2>&1 && sudo unzip -qq -o $NZ_DASHBOARD_PATH/app.zip -d $NZ_DASHBOARD_PATH && sudo mv $NZ_DASHBOARD_PATH/dashboard-linux-$os_arch $NZ_DASHBOARD_PATH/app && sudo rm $NZ_DASHBOARD_PATH/app.zip
    sudo chmod +x $NZ_DASHBOARD_PATH/app

    if [ "$os_alpine" != 1 ]; then
        sudo systemctl enable nezha-dashboard
        sudo systemctl restart nezha-dashboard
    else
        sudo rc-update add nezha-dashboard
        sudo rc-service nezha-dashboard restart
    fi
}

start_dashboard() {
    echo "> {{.StartDashboard}}"

    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        _cmd="start_dashboard_docker"
    elif [ "$IS_DOCKER_NEZHA" = 0 ]; then
        _cmd="start_dashboard_standalone"
    fi

    if eval "$_cmd"; then
        success "{{.SuccessStartDashboard}}"
    else
        err "{{.ErrorFailedToStart}}"
    fi

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

start_dashboard_docker() {
    sudo $DOCKER_COMPOSE_COMMAND -f ${NZ_DASHBOARD_PATH}/docker-compose.yaml up -d
}

start_dashboard_standalone() {
    if [ "$os_alpine" != 1 ]; then
        sudo systemctl start nezha-dashboard
    else
        sudo rc-service nezha-dashboard start
    fi
}

stop_dashboard() {
    echo "> {{.StopDashboard}}"

    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        _cmd="stop_dashboard_docker"
    elif [ "$IS_DOCKER_NEZHA" = 0 ]; then
        _cmd="stop_dashboard_standalone"
    fi

    if eval "$_cmd"; then
        success "{{.SuccessStopDashboard}}"
    else
        err "{{.ErrorFailedToStop}}"
    fi

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

stop_dashboard_docker() {
    sudo $DOCKER_COMPOSE_COMMAND -f ${NZ_DASHBOARD_PATH}/docker-compose.yaml down
}

stop_dashboard_standalone() {
    if [ "$os_alpine" != 1 ]; then
        sudo systemctl stop nezha-dashboard
    else
        sudo rc-service nezha-dashboard stop
    fi
}

show_dashboard_log() {
    echo "> {{printf .ViewLog "Dashboard"}}"

    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        show_dashboard_log_docker
    elif [ "$IS_DOCKER_NEZHA" = 0 ]; then
        show_dashboard_log_standalone
    fi

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

show_dashboard_log_docker() {
    sudo $DOCKER_COMPOSE_COMMAND -f ${NZ_DASHBOARD_PATH}/docker-compose.yaml logs -f
}

show_dashboard_log_standalone() {
    if [ "$os_alpine" != 1 ]; then
        sudo journalctl -xf -u nezha-dashboard.service
    else
        sudo tail -n 10 /var/log/nezha-dashboard.err
    fi
}

uninstall_dashboard() {
    echo "> {{.UninstallDashboard}}"

    if [ "$IS_DOCKER_NEZHA" = 1 ]; then
        uninstall_dashboard_docker
    elif [ "$IS_DOCKER_NEZHA" = 0 ]; then
        uninstall_dashboard_standalone
    fi

    clean_all

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

uninstall_dashboard_docker() {
    sudo $DOCKER_COMPOSE_COMMAND -f ${NZ_DASHBOARD_PATH}/docker-compose.yaml down
    sudo rm -rf $NZ_DASHBOARD_PATH
    sudo docker rmi -f ghcr.io/naiba/nezha-dashboard >/dev/null 2>&1
    sudo docker rmi -f registry.cn-shanghai.aliyuncs.com/naibahq/nezha-dashboard >/dev/null 2>&1
}

uninstall_dashboard_standalone() {
    sudo rm -rf $NZ_DASHBOARD_PATH

    if [ "$os_alpine" != 1 ]; then
        sudo systemctl disable nezha-dashboard
        sudo systemctl stop nezha-dashboard
    else
        sudo rc-update del nezha-dashboard
        sudo rc-service nezha-dashboard stop
    fi

    if [ "$os_alpine" != 1 ]; then
        sudo rm $NZ_DASHBOARD_SERVICE
    else
        sudo rm $NZ_DASHBOARD_SERVICERC
    fi
}

show_agent_log() {
    echo "> {{printf .ViewLog "Agent"}}"

    if [ "$os_alpine" != 1 ]; then
        sudo journalctl -xf -u nezha-agent.service
    else
        sudo tail -n 10 /var/log/nezha-agent.err
    fi

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

uninstall_agent() {
    echo "> {{.UninstallAgent}}"

    sudo ${NZ_AGENT_PATH}/nezha-agent service uninstall

    sudo rm -rf $NZ_AGENT_PATH
    clean_all

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

restart_agent() {
    echo "> {{.RestartAgent}}"

    sudo ${NZ_AGENT_PATH}/nezha-agent service restart

    if [ $# = 0 ]; then
        before_show_menu
    fi
}

clean_all() {
    if [ -z "$(ls -A ${NZ_BASE_PATH})" ]; then
        sudo rm -rf ${NZ_BASE_PATH}
    fi
}

show_usage() {
    echo "{{.Usage1}}"
    echo "--------------------------------------------------------"
    echo "{{.Usage2}}"
    echo "{{.Usage3}}"
    echo "{{.Usage4}}"
    echo "{{.Usage5}}"
    echo "{{.Usage6}}"
    echo "{{.Usage7}}"
    echo "{{.Usage8}}"
    echo "{{.Usage9}}"
    echo "--------------------------------------------------------"
    echo "{{.Usage10}}"
    echo "{{.Usage11}}"
    echo "{{.Usage12}}"
    echo "{{.Usage13}}"
    echo "{{.Usage14}}"
    echo "{{.Usage15}}"
    echo "--------------------------------------------------------"
}

show_menu() {
    printf "
    ${green}{{.MenuInfo}}${plain}
    --- https://github.com/naiba/nezha ---
    ${red}{{.UpdateWarning}}${plain}
    ${green}1.${plain}  {{.Menu1}}
    ${green}2.${plain}  {{.Menu2}}
    ${green}3.${plain}  {{.Menu3}}
    ${green}4.${plain}  {{.Menu4}}
    ${green}5.${plain}  {{.Menu5}}
    ${green}6.${plain}  {{.Menu6}}
    ${green}7.${plain}  {{.Menu7}}
    ————————————————-
    ${green}8.${plain}  {{.Menu8}}
    ${green}9.${plain}  {{.Menu9}}
    ${green}10.${plain} {{.Menu10}}
    ${green}11.${plain} {{.Menu11}}
    ${green}12.${plain} {{.Menu12}}
    ————————————————-
    ${green}13.${plain} {{.Menu13}}
    ————————————————-
    ${green}0.${plain}  {{.Menu0}}
    "
    echo && printf "{{.PromptMenu}}" && read -r num
    case "${num}" in
        0)
            exit 0
            ;;
        1)
            install_dashboard
            ;;
        2)
            modify_dashboard_config
            ;;
        3)
            start_dashboard
            ;;
        4)
            stop_dashboard
            ;;
        5)
            restart_and_update
            ;;
        6)
            show_dashboard_log
            ;;
        7)
            uninstall_dashboard
            ;;
        8)
            install_agent
            ;;
        9)
            modify_agent_config
            ;;
        10)
            show_agent_log
            ;;
        11)
            uninstall_agent
            ;;
        12)
            restart_agent
            ;;
        13)
            update_script
            ;;
        *)
            err "{{.ErrorPromptMenu}}"
            ;;
    esac
}

pre_check
installation_check

if [ $# -gt 0 ]; then
    case $1 in
        "install_dashboard")
            install_dashboard 0
            ;;
        "modify_dashboard_config")
            modify_dashboard_config 0
            ;;
        "start_dashboard")
            start_dashboard 0
            ;;
        "stop_dashboard")
            stop_dashboard 0
            ;;
        "restart_and_update")
            restart_and_update 0
            ;;
        "show_dashboard_log")
            show_dashboard_log 0
            ;;
        "uninstall_dashboard")
            uninstall_dashboard 0
            ;;
        "install_agent")
            shift
            if [ $# -ge 3 ]; then
                install_agent "$@"
            else
                install_agent 0
            fi
            ;;
        "modify_agent_config")
            modify_agent_config 0
            ;;
        "show_agent_log")
            show_agent_log 0
            ;;
        "uninstall_agent")
            uninstall_agent 0
            ;;
        "restart_agent")
            restart_agent 0
            ;;
        "update_script")
            update_script 0
            ;;
        *) show_usage ;;
    esac
else
    select_version
    show_menu
fi
