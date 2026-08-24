# Publishing Kairo as a PPA (Launchpad)

A Personal Package Archive lets apt users install and update Kairo with one
`add-apt-repository` line. Launchpad builds the package from source on its own
infrastructure, so you never hand a prebuilt binary to untrusted machines.

This guide is for the project owner. Users just need:

```bash
sudo add-apt-repository ppa:<owner>/kairo
sudo apt update
sudo apt install kairo
```

---

## 1. Prerequisites

- A Launchpad account (sign in at https://launchpad.net with an Ubuntu/Canonical SSO).
- A client machine running Ubuntu, with:

```bash
sudo apt install devscripts debhelper dh-golang dput
```

- `gpg` (installed by default on Ubuntu), plus your source tree and `make`.

## 2. Register the two keys Launchpad needs

Launchpad authentication uses two keys:

### a) OpenPGP signing key

Debian source packages must be gpg-signed.

```bash
gpg --full-generate-key        # RSA 4096, no expiry, "anjarman20 <email>"
gpg --keyserver keyserver.ubuntu.com --send-keys <KEYID>
```

Then in Launchpad: **Your name → OpenPGP keys → Import key**, paste your public
key, confirm the signed email.

### b) SSH upload key

Source uploads go over SSH. Generate and register:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/launchpad
```

In Launchpad: **Your name → SSH keys → Register an SSH key**, paste
`~/.ssh/launchpad.pub`.

Test connectivity:

```bash
ssh -i ~/.ssh/launchpad ppa.launchpad.net
# expect: "Welcome to Ubuntu PPA upload service..." then disconnect
```

## 3. Create the PPA

Launchpad web UI: **Your name → Create a new PPA**.

- Name: `kairo` (free on Launchpad; no conflict with AUR's URL-routing app).
- Display name / description: point at the GitHub repo and the `.deb` install doc.
- Enable **"Important"** processing if you want source uploads accepted quickly.

## 4. Source packaging

Launchpad compiles from source, so you need a `debian/` directory. This is
separate from the one-shot `make deb` binary package.

```
debian/control
debian/rules
debian/changelog
debian/compat
debian/copyright
debian/kairo.dirs
debian/kairo.install
debian/postinst
```

`debian/control`:

```
Source: kairo
Section: admin
Priority: optional
Maintainer: anjarman20 <anjarman20@users.noreply.github.com>
Build-Depends: debhelper (>= 13), dh-golang, golang-any,
               dh-sequence-golang
Standards-Version: 4.6.2
XS-Go-Import-Path: github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit

Package: kairo
Architecture: amd64
Depends: ${shlibs:Depends}, ${misc:Depends}
Recommends: ethtool, iproute2
Description: Linux performance analysis and optimization toolkit
 Detect current Linux configuration, analyze it against a selected workload
 profile, preview every change, then apply the smallest set of justified,
 reversible optimizations - and prove the result with benchmarks.
 Fully offline, no telemetry, no accounts, no cloud dependencies.
```

`debian/rules`:

```
#!/usr/bin/make -f
%:
	dh $@ --buildsystem=golang --with=golang
```

`debian/compat`:

```
13
```

`debian/kairo.dirs`:

```
usr/bin
var/lib/kairo
var/log/kairo
```

`debian/kairo.install`:

```
cmd/kairo usr/bin/
```

`debian/copyright` — copy `packaging/copyright` and add the `Files: debian/*`
stanza pointing at the same Apache-2.0 license.

`debian/postinst` — reuse `packaging/postinst` (creates `/var/lib/kairo`,
`/var/log/kairo`, default `/etc/kairo/config.yaml` without overwriting admin
edits).

Write a first `debian/changelog` entry:

```bash
dch --create -v 0.6.0-1 --package kairo "Initial PPA release."
```

## 5. Build and upload

Each release:

```bash
# Keep the same orig tarball name for version bumps:
tar --transform 's,^,kairo-0.6.0/,' -czf ../kairo_0.6.0.orig.tar.gz \
    --exclude='.git*' --exclude=bin --exclude=dist $(pwd)
debuild -S -d        # creates kairo_0.6.0-1_source.changes
dput ppa:<owner>/kairo kairo_0.6.0-1_source.changes
```

`-d` skips dependency checks so the tarball step alone feeds `debuild`.

Launchpad then builds amd64 and publishes to:

```
ppa:<owner>/kairo/ubuntu <release>
```

Track the build at your PPA page; failures surface the chroot build log there.

## 6. Install / update / roll back (client side)

```bash
sudo add-apt-repository ppa:<owner>/kairo
sudo apt update
sudo apt install kairo

# updates arrive as a normal:
sudo apt upgrade

# remove Kairo AND its PPA cleanly:
sudo ppa-purge -p kairo   # needs: apt install ppa-purge
sudo apt remove kairo
```

`ppa-purge` downgrades to the distro version if one exists, then drops the repo.

## 7. Maintenance notes

- **Version bumps**: rebuild the `.orig.tar.gz` only when upstream changes;
  caller version bumps (`0.6.1-1`) reuse `kairo_0.6.*.orig.tar.gz` naming rules.
- **Signing key expiry**: renew the OpenPGP subkey before it lapses; uploads
  fail hard once expired.
- **Ubuntu release coverage**: Launchpad rebuilds the same source for every
  supported series (jammy/noble/etc.) automatically.
- **Trust the build**: PPA binaries come from Launchpad's chroot, not from your
  laptop — no need to demand users trust a random `.deb`.

## 8. Relation to `make deb`

`make deb` produces the direct-install `.deb` (`dist/kairo_*.deb`) for manual
`dpkg -i`. The PPA flow is the scale-up path for many users. Keep both; they
share the same `packaging/` content.