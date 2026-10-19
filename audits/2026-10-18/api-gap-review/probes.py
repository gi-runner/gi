from pathlib import Path
import subprocess
root=Path('/tmp/gi-api-review-probes');root.mkdir(exist_ok=True)
cases={
 'null':("* def check = function(x){ return x === null }", "* def result = callonce read('helper.feature')\n* match result.check(null) == true"),
 'counter':("* def n = 0\n* def next = function(){ n++; return n }", "* def result = callonce read('helper.feature')\n* match result.next() == 1\n* match result.next() == 2"),
 'sibling':("* def helper = function(x){ return x + 1 }\n* def next = function(x){ return helper(x) }", "* def result = callonce read('helper.feature')\n* match result.next(2) == 3"),
 'deadline':("* def spin = function(){ while(true){} }", "* def result = callonce read('helper.feature')\nScenario: use later\n* def result = callonce read('helper.feature')\n* def value = result.spin()"),
 'captured-context':("* def name = function(){ return karate.info.scenarioName }", "* def result = callonce read('helper.feature')\nScenario: use later\n* def result = callonce read('helper.feature')\n* match result.name() == 'use later'"),
}
for name,(helper,body) in cases.items():
 d=root/name;d.mkdir(exist_ok=True)
 (d/'helper.feature').write_text('Feature: helper\nScenario: helper\n'+helper+'\n')
 (d/'main.feature').write_text('Feature: main\nScenario: first\n'+body+'\n')
 try:
  r=subprocess.run(['/tmp/gi-api-review-runner',str(d/'main.feature')],capture_output=True,text=True,timeout=3)
  print(name,r.stdout,r.stderr)
 except subprocess.TimeoutExpired:print(name,'PROCESS HUNG beyond 300ms run deadline; killed after 3s')
