# Phase B sample Python API fixture.
import json


def handler(req):
    return json.dumps({'ok': True, 'echo': req})


# end of file used by mempalace sample project behavioral test
