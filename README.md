# docker-sshd && kube-sshd

## docker-sshd

with `docker-sshd`, you can `ssh` into docker containers from anywhere, 
just like `docker exec -ti CONTAINER_ID /bin/bash` on the docker host machine.

```
+-------------+                                                                 
|             |    ssh CONTAINER1@docker-sshd      +--------------------+       
|     ops     +------------------------------------>                    |       
|             |                                    |    docker-sshd     |       
+-------------+                                    |                    |       
                                                   +----------------+---+       
                                                                    |           
                                                                    |           
                              docker exec -ti CONTAINER1 /bin/bash  |           
                                                                    |           
                +--------------------------------------------------------------+
                |                                                   |          |
                | Docker   +------------+  +------------+    +------v-----+    |
                |          |            |  |            |    |            |    |
                |          | CONTAINER3 |  | CONTAINER2 |    | CONTAINER1 |    |
                |          |            |  |            |    |            |    |
                |          +------------+  +------------+    +------------+    |
                |                                                              |
                +--------------------------------------------------------------+
```

## kube-sshd

with `kube-sshd`, you can `ssh` into kubenetes pod from anywhere, 
just like `kubectl exec -ti POD /bin/bash`.

```
+-------------+                                                                 
|             |    ssh POD1@kube-sshd              +--------------------+       
|     ops     +------------------------------------>                    |       
|             |                                    |    kube-sshd       |       
+-------------+                                    |                    |       
                                                   +----------------+---+       
                                                                    |           
                                                                    |           
                             kubectl exec -ti POD1 /bin/bash        |           
                                                                    |           
                +--------------------------------------------------------------+
                |                                                   |          |
                | k8s      +------------+  +------------+    +------v-----+    |
                |          |            |  |            |    |            |    |
                |          |    POD1    |  |    POD2    |    |    POD3    |    |
                |          |            |  |            |    |            |    |
                |          +------------+  +------------+    +------------+    |
                |                                                              |
                +--------------------------------------------------------------+
```

## Install

```
go get github.com/tg123/docker-sshd/cmd/docker-sshd
```

# Quick start

  1. start a container named `CONTAINER1`

    ```
    docker run -d -t --name CONTAINER1 ubuntu top
    bd78d93154cff5e8b40d19b1676670a49f582d2522384ecfe0d9e7d60846891e
    ```

  1. start `docker-sshd`

    ```
    docker-sshd
    ```

  1. connect to container with ssh

    ```
    ssh CONTAINER1@127.0.0.1 -p 2232
    root@bd78d93154cf:/#
    ```

## Options

```
--address value, -l value     listening address (default: "0.0.0.0")
--port value, -p value        listening port (default: 2232)
--server-key value, -i value  server key files, support wildcard (default: "/etc/ssh/ssh_host_ed25519_key")
--command value, -c value     default exec command (default: "/bin/sh")
```

### Docker related Environment

 * `DOCKER_HOST to` set the URL to the docker server, default unix:///var/run/docker.sock.
 * `DOCKER_API_VERSION` to set the version of the API to use, leave empty for latest.
 * `DOCKER_CERT_PATH` to specify the directory from which to load the TLS certificates (ca.pem, cert.pem, key.pem).
 * `DOCKER_TLS_VERIFY` to enable or disable TLS verification (off by default).

see <https://pkg.go.dev/github.com/docker/docker/client#FromEnv> for more detail

## Connecting from vscode

Make sure your container meet the [prerequisites](https://code.visualstudio.com/docs/remote/linux#_remote-host-container-wsl-linux-prerequisites).
Additionally, install [nc](https://linux.die.net/man/1/nc) to your container to have tcp redirect working