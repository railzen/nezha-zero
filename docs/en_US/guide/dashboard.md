---
outline: deep
---

# Install Dashboard

## Preparation

To set up Nezha Monitoring, you need:
1. A server with public internet access, with firewall and security policies allowing traffic on ports 8008 and 5555. These ports are necessary for accessing and receiving data. A server with a single core and 512MB of RAM is sufficient for most use cases.
2. A domain with an A record set to point to your Dashboard server IP.
::: tip 
If you want to use a CDN, prepare two domains: one configured with CDN for public access (CDN must support WebSocket protocol), and another domain not using CDN for communication between the Agent and Dashboard.  

This document uses "dashboard.example.com" and "data.example.com" as example domains.
:::
3. A Github account (or Gitlab, Gitee) only if you use OAuth login. No third-party account is required for username and password login.

::: tip Password-only login
This project supports administrator username and password login without OAuth. You do not need to create an OAuth application or provide a Client ID, Client Secret, or callback URL. Skip the OAuth/OIDC configuration below and go directly to [Installing the Dashboard on the Server](#installing-the-dashboard-on-the-server), then follow [Password-only login](#password-only).
:::

**This document uses the aaPanel for reverse proxying the Dashboard as an example. As future versions change, some features may change their entry points. This document is for reference only.**  
::: warning  
This project does not depend on the aaPanel; you can choose any server panel you prefer or manually install Nginx or Caddy to configure SSL and reverse proxy.  
If you do not need to use ports 80 and 443 to access the Dashboard, you can directly use the installation script to install and run Nezha Monitoring without installing Nginx.
:::  

## Obtaining Github Client ID and Secret

This section is only required for OAuth login. Nezha Monitoring can use Github, Gitlab, or Gitee as admin accounts.
1. First, create an OAuth application. For Github, log in to Github, open [Github OAuth Apps](https://github.com/settings/developers), and select "OAuth Apps" -> "New OAuth App".  
`Application name` - Fill in as you like.  
`Homepage URL` - Fill in with the domain for accessing the dashboard, such as "http://dashboard.example.com" (your domain).  
`Authorization callback URL` - Fill in with the callback address, such as "http://dashboard.example.com/oauth2/callback" (don't forget `/oauth2/callback`).  
1. Click “Register application”.  
2. Save the Client ID on the page, then click “Generate a new client secret” to create a new Client Secret, which will be displayed only once, **please keep it safe**.

## Using Cloudflare Access as an OAuth2 Provider

If you encounter issues using GitHub, GitLab, or Gitee for admin account logins, consider switching to [Cloudflare Access as your OAuth2 provider](/en_US/guide/q8.html) for authentication.

### Setting Up a New SaaS-OIDC Application

:::warning
The following steps are for users who have already started using Zero Trust. If you have not previously used Cloudflare Zero Trust, we strongly recommend that you first read the [Guide on Using Cloudflare Access as an OAuth2 Provider](/en_US/guide/q8.html) to understand the configuration examples and setup process.
:::

1. Go to [Zero Trust Dashboard](https://one.dash.cloudflare.com) and log in with your Cloudflare account.
2. `My Team` -> `Users` -> `<specific user>` -> Get `User ID` and save it.
3. `Access` -> `Application` -> `Add an Application`.
4. Choose `SaaS`, enter a custom application name in `Application` (e.g., nezha), select `OIDC`, and click `Add application`.
5. Select `Scopes`: `openid`, `email`, `profile`, `groups`.
6. Fill in your callback address in `Redirect URLs`, such as `https://dashboard.example.com/oauth2/callback`.
7. Save the `Client ID`, `Client Secret`, and `Issuer` address (protocol and domain part), e.g., `https://xxxxx.cloudflareaccess.com`.

**If using this method, after installing the Dashboard, modify the configuration file `/opt/nezha/dashboard/data/config.yaml`, and change the `Endpoint` configuration to the `Issuer` address saved earlier, e.g., `https://xxxxx.cloudflareaccess.com`, and restart the Dashboard.**

## OIDC Authentication Configuration (Optional)

Nazha supports custom OIDC authentication login. For configuration details, please refer to the documentation: [Enable OIDC authorization](/en_US/guide/q10.html).

## Installing the Dashboard on the Server

Run the installation script on the dashboard server:
```bash
curl -L https://raw.githubusercontent.com/railzen/nezha-zero/main/script/install_en.sh  -o nezha.sh && chmod +x nezha.sh && sudo ./nezha.sh
```  

Select the dashboard installation option and follow the script's environment setup prompts, then choose your login method.

### Password-only login {#password-only}

1. At `Configure OAuth login? [Y/n]:`, enter `n` and press Enter. Pressing Enter alone enables OAuth by default, so explicitly enter `n` here.
2. Enter an administrator username, such as `admin`. It cannot be empty and does not need to match an account on GitHub or another provider. Separate multiple usernames with commas.
3. Enter your panel password, or press Enter without a value to generate a random 16-character password. When OAuth is skipped, the script automatically configures password login without asking whether to enable it.
4. Enter the site title and ports as described below. After saving the configuration, the script displays the administrator username and password; save these credentials. `OAuth login: not configured` is expected for this installation method.
5. Once installation completes, open the dashboard's login page and sign in with the administrator username and password you configured. No third-party authorization is required.

### OAuth login (optional)

At `Configure OAuth login? [Y/n]:`, enter `y` or press Enter, then provide:

- `OAuth2 provider` - follow the script's choices; the default is `github`.
- `Client ID` and `Client Secret` - the credentials from your OAuth application.
- `Administrator username` - the username/User ID from the OAuth provider; separate multiple usernames with commas.
- `Configure password login? [Y/n]:` - press Enter or enter `y` to also configure password login, then set a panel password (leave it empty to generate one). Enter `n` to configure OAuth login only.

### Site settings and installation completion

Both login methods require the following site settings:

- `Site title` - custom site title.
- `Access port` - public access port, customizable, default is 8008.
- `Agent communication port` - port for Agent and Dashboard communication, default is 5555.

After inputting the values, wait for the image to be pulled.  
When the installation completes, you can access the dashboard by visiting your domain and port number, such as “http://dashboard.example.com:8008”.

In the future, if you need to run the script again, run:
```bash
./nezha.sh
``` 
to open the management script.  

## Configuring Reverse Proxy

Create a new site in the aaPanel, with the domain filled in as the public access domain, such as “http://dashboard.example.com”. Then click “Settings” to enter the site settings options, select “Reverse Proxy” - “New Reverse Proxy”.

Customize a proxy name and fill in `http://127.0.0.1` in the "Target URL" below, then click “Save”.

Open the “Configuration File” on the right side of the newly created reverse proxy and replace the configuration file with the following content:
```nginx
#PROXY-START/
location / {
    proxy_pass http://127.0.0.1:8008;
    proxy_set_header Host $http_host;
    proxy_set_header Upgrade $http_upgrade;
}
location ~ ^/(ws|terminal/.+|file/.+)$ {
    proxy_pass http://127.0.0.1:8008;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Upgrade";
    proxy_set_header Host $http_host;
}
#PROXY-END/
```
Click “Save”.  
Now you should be able to access the dashboard directly using the domain, such as “http://dashboard.example.com”.

### Additional Content:

CaddyServer v1 (v2 does not require special configuration):

```caddy
proxy /ws http://ip:8008 {
    websocket
    header_upstream -Origin
}
proxy /terminal/* http://ip:8008 {
    websocket
    header_upstream -Origin
}
proxy /file/* http://ip:8008 {
    websocket
    header_upstream -Origin
}
```

## Configuring SSL in the aaPanel

First, temporarily disable the reverse proxy.  
Like configuring SSL certificates for other websites, enter the “SSL” in the site settings, and you can choose to automatically apply for a Let’s Encrypt certificate or manually configure an existing certificate.  
If you use GitHub OAuth login, after completing the SSL settings, go back to [Github OAuth Apps](https://github.com/settings/developers) and edit the previously created OAuth application. Change "Homepage URL" and "Authorization callback URL" from `http` to `https`, such as "https://dashboard.example.com" and "https://dashboard.example.com/oauth2/callback". **Failing to change this may prevent OAuth login.** Password-only login does not require an OAuth callback URL.

## Updating the Dashboard

Run the script `./nezha.sh`, and select to restart and update the dashboard.
