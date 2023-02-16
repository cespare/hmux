# TODO

* Add some benchmarks
  - Routing
  - Param extraction
* Check allocs, reduce
* Switch to a tree-based implementation
* Look into whether we should redirect trailing slashes
  - In some projects the lack of redirect is a problem
  - httprouter has a configuration knob for this
  - Check what the others do
