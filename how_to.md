From [GitHub and Go: forking, pull requests, and go-getting](http://blog.campoy.cat/2014/03/github-and-go-forking-pull-requests-and.html)

```
$ go get github.com/letsencrypt/boulder

$ git remote add fork git@github.com:joanasancho/boulder.git

$ git pull fork master

$ docker-compose up
```


```
$ docker build -t certbot-test ./certbot-test

$ docker run -it -p 5001:5001 -p 5002:5002 --net host certbot-test certonly -a standalone -d <your_local_domain_name> --server http://<your_boulder_hostst>:4000/directory    

```