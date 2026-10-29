import concurrent.futures
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SCRIPT=Path(__file__).resolve().parents[1]/'skills/work-schedule-sync/scripts/sync_queue.py'
spec=importlib.util.spec_from_file_location('sync_queue',SCRIPT)
queue=importlib.util.module_from_spec(spec)
spec.loader.exec_module(queue)

class QueueTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.root=Path(self.tmp.name)
 def tearDown(self):self.tmp.cleanup()
 def payload(self,request='one',project='project'):
  return {'request_id':request,'project_id':project,'changes':[{'external_key':'TASK','expected_version':1,'status':'in_progress'}]}
 def test_enqueue_never_calls_network_and_replay_is_deduplicated(self):
  with patch.object(queue,'send',side_effect=AssertionError('network not allowed')):
   result=queue.enqueue(self.root,self.payload());again=queue.enqueue(self.root,self.payload())
  self.assertTrue(result['queued']);self.assertFalse(result['synced']);self.assertEqual(again['request_id'],'one');self.assertEqual(len(queue.jobs(self.root)),1)
  changed=self.payload();changed['changes'][0]['status']='blocked'
  with self.assertRaises(RuntimeError):queue.enqueue(self.root,changed)
 def test_retry_preserves_exact_payload_then_clears_business_text(self):
  payload=self.payload();queue.enqueue(self.root,payload);sent=[]
  def fail(body):sent.append(body);return None,{'code':'network_error','message':'timeout','retryable':True}
  queue.process_once(self.root,fail);path,job=queue.jobs(self.root)[0];self.assertEqual(job['state'],'retry');self.assertEqual(job['payload'],payload);job['next_attempt_at']=0;queue.atomic_write(path,job)
  def succeed(body):sent.append(body);return {'request_id':'one','project_id':'project','results':[{'id':'task','version':2}]},None
  queue.process_once(self.root,succeed);job=queue.jobs(self.root)[0][1];self.assertEqual(sent,[payload,payload]);self.assertEqual(job['state'],'synced');self.assertNotIn('payload',job);self.assertTrue(queue.enqueue(self.root,payload)['synced'])
 def test_conflict_blocks_only_its_project_and_requires_explicit_resolution(self):
  queue.enqueue(self.root,self.payload('conflict','A'));queue.enqueue(self.root,self.payload('other','B'))
  def sender(body):
   if body['project_id']=='A':return None,{'code':'conflict','message':'stale','retryable':False}
   return {'request_id':body['request_id'],'project_id':'B','results':[]},None
  queue.process_once(self.root,sender);states={j['request_id']:j['state'] for _,j in queue.jobs(self.root)};self.assertEqual(states,{'conflict':'conflict','other':'synced'})
  replacement=self.payload('replacement','A');replacement['changes'][0]['expected_version']=2;queue.enqueue(self.root,replacement);queue.resolve(self.root,'conflict','replacement')
  queue.process_once(self.root,lambda body:({'request_id':body['request_id'],'project_id':body['project_id'],'results':[]},None));states={j['request_id']:j['state'] for _,j in queue.jobs(self.root)};self.assertEqual(states['conflict'],'superseded');self.assertEqual(states['replacement'],'synced')
 def test_full_queue_does_not_delete_existing_jobs(self):
  queue.enqueue(self.root,self.payload())
  with patch.object(queue,'MAX_JOBS',1):
   with self.assertRaises(RuntimeError):queue.enqueue(self.root,self.payload('two'))
  self.assertEqual(len(queue.jobs(self.root)),1)
 def test_concurrent_enqueue_does_not_overwrite_files(self):
  with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:list(pool.map(lambda n:queue.enqueue(self.root,self.payload(str(n))),range(12)))
  self.assertEqual(len(queue.jobs(self.root)),12)
 def test_credentials_are_not_persisted(self):
  with patch.dict('os.environ',{'WORK_SCHEDULE_TOKEN':'private-test-token'}):queue.enqueue(self.root,self.payload())
  for path in self.root.glob('*.json'):self.assertNotIn('private-test-token',path.read_text())


class HTTPWorkerTests(unittest.TestCase):
 def test_real_worker_and_redirect_protection(self):
  import http.server,threading,subprocess,os,time
  requests=[]
  class Handler(http.server.BaseHTTPRequestHandler):
   def do_POST(self):
    body=json.loads(self.rfile.read(int(self.headers['Content-Length'])));requests.append(self.headers.get('Authorization'))
    if body['request_id']=='redirect':self.send_response(302);self.send_header('Location','http://untrusted.invalid/');self.end_headers();return
    if body['request_id']=='slow':time.sleep(2.4)
    data=json.dumps({'request_id':body['request_id'],'project_id':body['project_id'],'results':[{'id':'TASK','version':2}]}).encode()
    try:self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(data)
    except (BrokenPipeError,ConnectionResetError):pass
   def log_message(self,*args):pass
  server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
  try:
   with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ,{'WORK_SCHEDULE_URL':'http://127.0.0.1:'+str(server.server_port),'WORK_SCHEDULE_TOKEN':'private-test-token'}):
    root=Path(directory);payload={'request_id':'real','project_id':'project','changes':[{'external_key':'TASK','expected_version':1,'status':'in_progress'}]};queue.enqueue(root,payload)
    result=subprocess.run([os.sys.executable,str(SCRIPT),'worker','--queue-dir',directory,'--once'],capture_output=True,text=True,timeout=5)
    self.assertEqual(result.returncode,0,result.stdout);self.assertEqual(queue.jobs(root)[0][1]['state'],'synced');self.assertEqual(requests,['Bearer private-test-token'])
    redirect=dict(payload,request_id='redirect');_,error=queue.send(redirect);self.assertFalse(error['retryable']);self.assertEqual(len(requests),2)
    slow=dict(payload,request_id='slow');start=time.monotonic();_,error=queue.send(slow);self.assertTrue(error['retryable']);self.assertLess(time.monotonic()-start,3)
  finally:server.shutdown();server.server_close()

if __name__=="__main__":unittest.main()
