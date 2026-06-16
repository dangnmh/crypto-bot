# Step-by-Step Guide: Connect Local Machine to Remote k3d Cluster

Since you are connected to the remote server via **Radmin VPN** and have **SSH** access, you have two primary methods to connect your local `kubectl` to the remote `k3d` Kubernetes cluster.

---

## Method 1: SSH Port Forwarding (Recommended & Easiest)
This is the most secure and easiest method. It does not require modifying the k3d cluster configuration or recreating the cluster to add TLS certificates. You simply tunnel the Kubernetes API server port through SSH.

### Step 1: Find the Kubernetes API Server Port
1. SSH into your remote server.
2. Run the following command to see your cluster details and identify the port k3d is using for the API server:
   ```bash
   docker ps | grep k3d-
   ```
   Look for the port mapped to container port `6443` (e.g., `0.0.0.0:38423->6443/tcp`). The port on the host (e.g., `38423`) is the **API port**.

### Step 2: Extract the Kubeconfig from the Remote Server
On the remote server, export the kubeconfig file for your cluster:
```bash
k3d kubeconfig get <cluster-name> > k3d-kubeconfig.yaml
```
*(Replace `<cluster-name>` with your actual k3d cluster name, usually `k3s-default` if you didn't specify one).*

### Step 3: Copy the Kubeconfig to your Local Machine
From your **local machine** (not the remote server), copy the file over using `scp` (or copy-paste the text):
```bash
scp user@SERVER_VPN_IP:/path/to/k3d-kubeconfig.yaml ~/.kube/config-remote
```
*(Replace `SERVER_VPN_IP` with the server's Radmin VPN IP, and update the path).*

### Step 4: Establish the SSH Port Forwarding Tunnel
From your **local machine**, run this command in a background terminal to keep the tunnel open:
```bash
ssh -N -L <API-port>:127.0.0.1:<API-port> user@SERVER_VPN_IP

ssh -v -p 2222 -N -L 39373:127.0.0.1:39373 dangnmh@26.128.244.94
```
*Example (if your API port from Step 1 is `38423`):*
```bash
ssh -N -L 38423:127.0.0.1:38423 user@SERVER_VPN_IP
```
- `-N`: Do not execute a remote command (useful for just port forwarding).
- `-L`: Forward local port to the remote host/port.

### Step 5: Test the Connection
Now, in another terminal window on your **local machine**, set your kubeconfig context and run:
```bash
# export KUBECONFIG=~/.kube/config-remote
export KUBECONFIG=./deploy/k8s/k3d-kubeconfig.yaml

kubectl get nodes
```
You should now successfully see the nodes of your remote k3d cluster.

---

## Method 2: Direct Access via Radmin VPN IP (No SSH Tunnel needed)
If you want to connect directly using the Radmin VPN IP address without having to keep an SSH tunnel open, you need to expose the API server to the VPN network and add the VPN IP to the cluster's TLS certificate.

### Step 1: Create the Cluster with TLS SAN
By default, the Kubernetes API server TLS certificate is only valid for `localhost` and `127.0.0.1`. To access it using the VPN IP, you must specify the VPN IP as a Subject Alternative Name (SAN) when creating the cluster.

On the **remote server**, create the cluster using:
```bash
k3d cluster create <cluster-name> \
  --api-port SERVER_VPN_IP:6443 \
  --k3s-arg "--tls-san=SERVER_VPN_IP@server:*"
```
*(Replace `SERVER_VPN_IP` with your actual Radmin VPN IP address).*

### Step 2: Get the Kubeconfig on the Local Machine
1. Export the kubeconfig on the server:
   ```bash
   k3d kubeconfig get <cluster-name> > k3d-kubeconfig-direct.yaml
   ```
2. Copy the file to your **local machine**:
   ```bash
   scp user@SERVER_VPN_IP:/path/to/k3d-kubeconfig-direct.yaml ~/.kube/config-direct
   ```

### Step 3: Modify Kubeconfig Server URL locally
Open the copied `~/.kube/config-direct` file on your **local machine** and find the `server:` field under `clusters`:
```yaml
clusters:
- cluster:
    certificate-authority-data: ...
    server: https://127.0.0.1:6443
```
Change `127.0.0.1` to the server's Radmin VPN IP:
```yaml
    server: https://SERVER_VPN_IP:6443
```

### Step 4: Verify the Connection
On your **local machine**, run:
```bash
export KUBECONFIG=~/.kube/config-direct
kubectl get nodes
```
Because the TLS SAN was correctly registered, `kubectl` will connect and authenticate securely over the Radmin VPN link.