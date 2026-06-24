# tsnames

## Name

*tsnames* - serves DNS records for Tailscale nodes from a configurable local zone.

## Description

The *tsnames* plugin automatically populates DNS A and AAAA records for every machine on your
Tailnet within a local zone of your choosing. It also supports CNAME records defined via Tailscale
node tags, allowing logical names to point to Tailscale machines.

The plugin uses the local Tailscale socket to watch for netmap changes, so DNS records stay
up-to-date as nodes join, leave, or change addresses without any external API tokens.

## Syntax

~~~ txt
tsnames ZONE
~~~

## Examples

Serve the Tailnet on `example.com`:

``` txt
example.com {
    tsnames example.com
}
```

With the above configuration, a machine named `test-machine` on the Tailnet will have A and AAAA
records for `test-machine.example.com` served automatically.

### CNAME records via Tags

A CNAME record can be added to point to a machine by applying a Tailscale machine tag prefixed
by `cname-`. For example, the tag `cname-friendly-name` on the `test-machine` node will result
in:

~~~
friendly-name IN CNAME test-machine.example.com.
test-machine  IN A <Tailscale IPv4 Address>
test-machine  IN AAAA <Tailscale IPv6 Address>
~~~
