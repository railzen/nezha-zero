---
outline: deep
---

# Quick Start

Install the Nezha Zero dashboard on a Linux server and sign in with an administrator username and password. No GitHub account or OAuth application is required.

The dashboard also supports [OAuth login](#oauth-login), which verifies administrator identity through a provider such as GitHub. You can use OAuth alone or alongside password login. To use it, prepare an OAuth application before selecting OAuth during installation.

## Prerequisites

- A Linux server with access to GitHub and root or sudo privileges.
- The `curl` command available on the server.
- Open the dashboard and Agent communication ports: TCP `8008` and `5555` by default. Check your cloud security group as well as the server firewall.

You can start with the server's IP address and configure a domain and reverse proxy later.

## Choose an installation method

Choose either method below. Both use the same installer: Docker runs the dashboard in a container, while standalone installation runs it directly as a system service.

### Docker installation

Run on your server:

```bash
curl -L https://raw.githubusercontent.com/railzen/nezha-zero/main/script/install_en.sh -o nezha.sh && chmod +x nezha.sh && sudo ./nezha.sh install_dashboard
```

At the installation method prompt, select `1. Docker` by entering `1`. On a fresh installation, the script attempts to install Docker if it is missing. Existing Docker installations need either `docker compose` or `docker-compose`; if the script reports missing Compose, install it before running the installer again.

Continue with [Configure password login](#password-login) or [Configure OAuth login](#oauth-login) below. The script will pull the dashboard image and start the container.

### Direct installation (standalone)

Run on your server:

```bash
curl -L https://raw.githubusercontent.com/railzen/nezha-zero/main/script/install_en.sh -o nezha.sh && chmod +x nezha.sh && sudo ./nezha.sh install_dashboard
```

At the installation method prompt, select `2. Standalone` by entering `2`. Docker is not required. The script downloads the dashboard binary and configures a system service (normally systemd, or OpenRC on Alpine).

Continue with [Configure password login](#password-login) or [Configure OAuth login](#oauth-login) below. Both methods store installation data under `/opt/nezha/dashboard`. If a dashboard is already installed, the script may detect and reuse its installation method without showing this prompt.

## Configure password login {#password-login}

1. At `Configure OAuth login? [Y/n]:`, enter `n` and press Enter. Do not just press Enter, as the default enables OAuth configuration.
2. Enter an administrator username, such as `admin`. It cannot be empty and does not need to match a third-party account.
3. Enter a panel password, or leave it empty to generate a random 16-character password. Skipping OAuth automatically configures password login.
4. Enter a site title, such as `My Monitoring`. It cannot be empty.
5. Enter the dashboard access port, or press Enter for the default `8008`.
6. Enter the Agent RPC port, or press Enter for the default `5555`.

After saving the configuration, the script displays the administrator username, password, and ports. Save these details. `OAuth login: not configured` is expected for password-only installation. Wait for the installer to finish and start the dashboard.

## Configure OAuth login (optional) {#oauth-login}

OAuth login redirects you to a third-party provider for authorization and then returns you to the dashboard. It requires an account with that provider and an OAuth application. Skip this section if you only want password login.

1. Before installation, follow [Obtaining GitHub Client ID and Secret](/en_US/guide/dashboard.html#obtaining-github-client-id-and-secret) to create an application, save its Client ID and Client Secret, and set the dashboard callback URL. Alternatively, see [Cloudflare Access configuration](/en_US/guide/q8.html) or [OIDC configuration](/en_US/guide/q10.html).
2. At `Configure OAuth login? [Y/n]:`, enter `y` or press Enter, then provide the OAuth provider, Client ID, and Client Secret.
3. Enter the administrator username/User ID from your OAuth provider, separating multiple values with commas. Use the identity from the provider, not an arbitrary local username.
4. At `Configure password login? [Y/n]:`, enter `n` for OAuth-only login. Enter `y` or press Enter to also configure password login and set a panel password.
5. Enter the site title, dashboard port, and Agent RPC port, then wait for installation to finish. See [Install Dashboard](/en_US/guide/dashboard.html) for further details.

## Open the dashboard

Visit `http://SERVER_IP:8008` in your browser, using your chosen port if you changed it. On the dashboard login page, sign in with the administrator username and password you configured.

If you configured OAuth, you can use the OAuth login entry on the login page to authorize with your provider and return to the dashboard. Use this entry if you configured OAuth only.

To use a domain and HTTPS, follow [Configuring Reverse Proxy](/en_US/guide/dashboard.html#configuring-reverse-proxy) and [Configuring SSL](/en_US/guide/dashboard.html#configuring-ssl-in-the-aapanel). Password-only login does not require an OAuth callback URL.

## Connect your first server

1. In dashboard settings, enter the dashboard domain or IP that Agents can reach directly. If using a domain, do not proxy it through a CDN.
2. Add a server in the Servers page and copy the one-click installation command for its operating system.
3. Run the command on the monitored server and wait for it to appear online in the dashboard.

See [Install Agent](/en_US/guide/agent.html) for details. To update the dashboard or view logs later, run `sudo ./nezha.sh` from the directory where you saved the script to open its management menu.
