# TODO

* Add some benchmarks
  - Routing
  - Param extraction
* Check allocs, reduce
* Switch to a tree-based implementation
* Look into whether we should redirect trailing slashes
  - In some projects the lack of redirect is a problem
  - httprouter has a configuration knob for this
  - chi provides two middlewares to help: StripSlashes and RedirectSlashes
* Should we make it easier to handle HEAD requests when GET are supported?
  - If you use `Get` as normal, it's typical to construct a server that only
    responds to GET requests and not HEAD requests.
  - We could automatically register HEAD as well, but it doesn't seem obvious
    that that would be a good idea.
  - httprouter doesn't seem to have any facility to help with this.
  - chi provides the `GetHead` middleware which routes HEAD requests (if they
    don't already match) to a matching GET route.
