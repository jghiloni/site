+++
categories = ["blog"]
date = 2024-07-19T14:15:18-04:00
description = "Running Dagger in Github Actions, on self-hosted runners, with a private repo (and a partridge in a pear tree)"
draft = false
tags = ["tech", "ci/cd", "k8s"]
title = "Daggerize Everything"
showComments = true

[params]
  skeet = "at://did:plc:v757ur25azanhxnmg537f6pv/app.bsky.feed.post/3kxsvhcl7jp2k"

+++

I've been out of work since the beginning of June, so I've been interviewing
like crazy. One place I interviewed was [dagger](https://dagger.io), a CI/CD-
as-code system that works the same locally as it does in your favorite CI/CD
workflow engine (Jenkins, Github Actions, or my personal favorite, [Concourse](https://concourse-ci.org)).
It was started by some ex-Docker engineers and you can see the pedigree in
how they approach isolation.

Part of the interview process was a take-home project. I knocked it out in a day,
and trying to add a bit of panache, decided to use Dagger to build it and store
a Docker image in my homelab-hosted [Harbor](https://goharbor.io) registry.
Writing the Dagger code to do that was trivial, following their _excellent_
documentation.

Storing the image in my private registry meant that I couldn't use Github's
hosted Actions Runners, the VMs that run the jobs defined in your action
workflows. Fortunately, they provide the software to run self-hosted runners
inside your private networks; Github doesn't have to be able to reach them,
they only have to be able to reach Github.

They offer the software in a few form factors, but since I run everything
in a Kubernetes cluster, I decided to opt for their Kubernetes operator and
runner set. I was able to deploy the listener controller and a runner
autoscaling set using the official documentation. That was the end of the
easy path.

The first couple of times I ran the official Dagger Github Action, it failed
almost immediately and with no error message. Fortunately, I had an interview
that day that was designed for a pair programming session on something in the
backlog. After talking with the engineer, we decided to work on this problem.
It turns out it was mostly the ergonomics of the action--it's not that there
was no error, it's just the command it was running (calling `curl` to download
the `dagger` binary into the workspace for use in the next step) was piping its
`stderr` stream to `/dev/null`, effectively throwing it away. We removed that
and did a couple more optimization steps, and I submitted the pull request,
which has since been merged.

It turns out it was complaining because `curl` didn't exist on the OS running
the action. This was really surprising to me, because I expected the `ubuntu`
runners they use on their hosted runners to use the same OS image as the self-
hosted variant, but it turns out that they don't.

Fair enough, let's add a step to manually add `curl` to my workflow. That failed
because it couldn't install the package properly, even though it worked when I
tried manually. Turns out I needed to manually create the `/usr/share/man/man1`
directory where the `curl` APT package tries to put files. Odd that they don't
create the directories during `apt-get install` but ... not my circus, not my
monkeys.

Once I added that, it started installing dagger, and failing in a new place.
![Progress!](./progress.jpg)
This time, it was trying to run `git` commands locally. Was `git` installed?
NOPE.

Ok, let's add `git` to the list of things to install, and make sure we use the
`GITHUB_TOKEN` environment variable to ensure that we can access our private
repo. Well, now it's failing somewhere new. This error is a little more esoteric.
As a close to this part of the story, I was overthinking things. Because I used
the official `actions/checkout` action, I already had the code I needed.

Turns out, after some debugging, it's because dagger can't start the docker daemon.
Well, duh. It's in a docker container! However, there is a relatively common--
if completely cursed--pattern of using Docker-in-Docker. Oh geez, how hard is it
going to be to shoehorn that into these self-hosted runners? Why hadn't they
thought of this!?

Turns out, they had. It just wasn't mentioned in the documentation, and it was
off by default. After digging through the code (my preferred way of solving these
kinds of problems even though it should **not** be required), I found the proper
incantation to install things. Where the documented command looked like

```bash
$ INSTALLATION_NAME=arc-runner-set
$ GITHUB_CONFIG_URL=https://github.com/jghiloni/myprivaterepo.git
$ GITHUB_PAT=ghpat_lolnicetry
$ NAMESPACE=arc-runners
$ helm install ${INSTALLATION_NAME} \
--namespace ${NAMESPACE} --create-namespace \
--set githubConfigUrl=${GITHUB_CONFIG_URL} \
--set githubConfigSecret.github_token=${GITHUB_PAT} \
oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set
```

for my case, it should have looked like

```bash
$ INSTALLATION_NAME=arc-runner-set
$ GITHUB_CONFIG_URL=https://github.com/jghiloni/myprivaterepo.git
$ GITHUB_PAT=ghpat_lolnicetry
$ NAMESPACE=arc-runners
$ helm install ${INSTALLATION_NAME} \
--namespace ${NAMESPACE} --create-namespace \
--set githubConfigUrl=${GITHUB_CONFIG_URL} \
--set githubConfigSecret.github_token=${GITHUB_PAT} \
--set containerMode.type=dind \ # <-- hey look here
oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set
```

Once I got that working, everything was kosher. All in all, this was about
3 hours of debugging and iterating. I hope that even if you don't find it
interesting, someone finds it useful!
