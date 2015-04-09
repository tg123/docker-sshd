# Docker-sshd

with `docker-sshd`, you can `ssh` into docker containers from anywhere, 
just like `docker exec -ti CONTAINER_ID /bin/bash` the docker host machine.

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



# Install

```
go get github.com/tg123/docker-sshd
```

# Quick start

  1. start a container named `CONTAINER1`

    ```
    docker run -d -t --name CONTAINER1 ubuntu top
    bd78d93154cff5e8b40d19b1676670a49f582d2522384ecfe0d9e7d60846891e
    ```

  1. start `docker-sshd`

    ```
    $GOPATH/bin/docker-sshd
    ```

  1. connect to container with ssh

    ```
    ssh CONTAINER1@127.0.0.1 -p 2232
    root@bd78d93154cf:/#
    ```



# Configuration

```
  -c, --command=/bin/bash                       default exec command
  -H, --host=unix:///var/run/docker.sock        docker host socket
  -h, --help=false                              Print help and exit
  -i, --server_key=/etc/ssh/ssh_host_rsa_key    Key file for docker-sshd
  -l, --listen_addr=0.0.0.0                     Listening Address
  -p, --port=2232                               Listening Port
  --version=false                               Print version and exit
```

