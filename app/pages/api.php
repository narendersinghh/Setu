<?php
$do=$_GET['do']??'';
session_write_close();
if($do==='tick'){
  $n=0;
  Cron::kick();
  // Short jobs only here; AI audits/reports run in detached background workers (no web-server time limits)
  if(is_post()&&Queue::due())$n=Queue::work(8,true);
  if(is_post())Cron::spawn();
  if(is_post()&&time()-(int)setting('scan_last','0')>120){Settings::set('scan_last',time());try{Automation::scan();Booking::remind();}catch(Throwable $e){}}
  $s=DB::row("SELECT SUM(status='pending' AND run_after<=NOW()) due,SUM(status='pending' AND run_after>NOW()) later,SUM(status='running') running FROM jobs WHERE status IN ('pending','running')");
  $next=DB::val("SELECT MIN(run_after) FROM jobs WHERE status='pending' AND run_after>NOW()");
  json_out(['done'=>$n,'due'=>(int)$s['due'],'later'=>(int)$s['later'],'running'=>(int)$s['running'],'next'=>$next?max(1,strtotime($next)-time()):null]);}
// Live process log of one lead (View process popup and the lead page progress box)
if($do==='process'){
  $l=DB::row('SELECT id,name,stage,progress,progress_msg,last_error,is_lead,assigned_to FROM leads WHERE id=?',[(int)($_GET['id']??0)]);
  if(!$l||!lead_ok($l)||(!$l['is_lead']&&!can('prospecting')))json_out(['error'=>'forbidden'],403);
  $busy=in_array($l['stage'],['queued','auditing','reporting','contacting'],true);
  $o=['id'=>(int)$l['id'],'name'=>$l['name'],'stage'=>$l['stage'],'stage_label'=>stages()[$l['stage']][0]??$l['stage'],'busy'=>$busy,'progress'=>(int)$l['progress'],'msg'=>$l['progress_msg'],'error'=>$l['stage']==='failed'?$l['last_error']:'','can_abort'=>can('settings')];
  if(empty($_GET['lite']))$o+=Trace::view($l['id']);
  json_out($o);}
if($do==='mail_sync'&&is_post()){$n=0;if(Mailer::imapOn()&&time()-(int)setting('imap_last','0')>45){Settings::set('imap_last',time());try{$n=Outreach::syncInbox();}catch(Throwable $e){json_out(['n'=>0,'error'=>$e->getMessage()]);}}json_out(['n'=>$n]);}
if($do==='prices'){if(!can('settings'))json_out(['error'=>'forbidden'],403);json_out(jd(setting('price_update')));}
if($do==='activity'){
  if(!can('settings'))json_out(['error'=>'forbidden'],403);
  $active=DB::all("SELECT id,name,stage,progress,progress_msg,last_error,source FROM leads WHERE stage IN ('queued','auditing','reporting','contacting') ORDER BY last_activity DESC LIMIT 60");
  $failed=DB::all("SELECT id,name,last_error,last_activity FROM leads WHERE stage='failed' AND last_activity>NOW()-INTERVAL 2 DAY ORDER BY last_activity DESC LIMIT 15");
  $search=DB::all("SELECT id,keyword,location,found,limit_n FROM campaigns WHERE status='searching' ORDER BY id DESC LIMIT 10");
  $sched=DB::all("SELECT j.ref_id id,l.name,j.run_after FROM jobs j JOIN leads l ON l.id=j.ref_id WHERE j.type='contact' AND j.status='pending' ORDER BY j.run_after LIMIT 30");
  $runs=DB::all("SELECT r.id,r.status,r.next_at,a.name,l.name lead FROM automation_runs r JOIN automations a ON a.id=r.automation_id JOIN leads l ON l.id=r.lead_id WHERE r.status IN ('running','waiting') ORDER BY r.updated_at DESC LIMIT 30");
  json_out(['active'=>$active,'failed'=>$failed,'search'=>$search,'scheduled'=>$sched,'runs'=>$runs,'now'=>time()]);}
if(is_post()){
  if($do==='status'){$id=(int)$_POST['id'];$st=$_POST['status']??'';if(isset(crm_statuses()[$st])&&lead_ok(DB::row('SELECT id,is_lead,assigned_to FROM leads WHERE id=?',[$id])))CRM::setStatus($id,$st);json_out(['ok'=>1]);}
  if(!can('settings'))json_out(['error'=>'forbidden'],403);
  if($do==='abort'){
    $ids=($_POST['all']??'')==='1'?array_column(DB::all("SELECT id FROM leads WHERE stage IN ('queued','auditing','reporting','contacting')"),'id'):array_map('intval',(array)($_POST['ids']??[]));
    $n=Queue::abort($ids);
    if(($_POST['all']??'')==='1'){DB::q("UPDATE campaigns SET status='stopped' WHERE status='searching'");DB::q("UPDATE jobs SET status='cancelled' WHERE status='pending' AND type IN ('search','contact')");}
    json_out(['ok'=>1,'n'=>$n]);}
  if($do==='unschedule'){DB::q("UPDATE jobs SET status='cancelled' WHERE type='contact' AND status='pending' AND ref_id=?",[(int)$_POST['id']]);event((int)$_POST['id'],'note','Scheduled outreach cancelled');json_out(['ok'=>1]);}
  if($do==='cancelrun'){Automation::cancel((int)$_POST['id']);json_out(['ok'=>1]);}
  if($do==='toggle_auto'){DB::update('automations',['active'=>(int)!empty($_POST['on'])],'id=?',[(int)$_POST['id']]);json_out(['ok'=>1]);}}
json_out(['error'=>'unknown'],404);
