<?php
// Rule-based automations: trigger → conditions → steps (WhatsApp, email, AI call, booking link, wait, status, tag…)
final class Automation{
  static function triggers(){return ['lead_created'=>['New lead added','Any new lead in Leads (form, ads, webhook, manual, import or moved from scraped data).','Source (optional)'],'status_changed'=>['Status changes to','When a lead moves to a status.','Status'],'tag_added'=>['Tag added','When a tag is added to a lead.','Tag'],'replied'=>['Lead replied','When a reply is detected (email, WhatsApp) or logged.',''],'report_ready'=>['Audit report ready','When the PDF audit report is generated.',''],'appointment_booked'=>['Appointment booked','When a meeting is booked (booking page or manually).',''],'call_completed'=>['AI call finished','When an AI voice call ends.',''],'no_reply'=>['No reply after','Contacted but no reply after N days.','Days'],'follow_up_due'=>['Follow-up date reached','When the lead\'s follow-up date/time arrives.','']];}
  static function actions(){return ['whatsapp'=>['Send WhatsApp','wa','green'],'email'=>['Send email','mail','blue'],'call'=>['AI voice call','mic','purple'],'booking'=>['Send booking link','cal','orange'],'wait'=>['Wait','clock','gray'],'status'=>['Change status','target','teal'],'tag'=>['Add tag','tag','gray'],'assign'=>['Assign to user','user','gray'],'followup'=>['Set follow-up','clock','gray'],'notify'=>['Notify team (email)','alert','red'],'report'=>['Run audit & report','file','gold'],'webhook'=>['Send to webhook','plug','teal']];}
  static function templates(){return [
    'welcome'=>['name'=>'Instant WhatsApp welcome','desc'=>'Reply to every new enquiry within seconds and alert your team.','i'=>'wa','c'=>'g','trig'=>'lead_created','steps'=>[['t'=>'whatsapp','text'=>"Hi {{first_name}} 👋 Thanks for contacting {{agency_name}}! We have received your enquiry and will call you shortly.\n\nPrefer a time that suits you? Book here: {{booking_link}}"],['t'=>'notify','subject'=>'New lead: {{contact_name}} ({{business_name}})','text'=>'New lead: {{contact_name}} – {{phone}} – {{email}}'],['t'=>'status','v'=>'contacted']]],
    'aicall'=>['name'=>'Speed-to-lead AI call','desc'=>'An AI voice agent calls new ad leads within 2 minutes to qualify them.','i'=>'mic','c'=>'p','trig'=>'lead_created','tv'=>'facebook','steps'=>[['t'=>'wait','n'=>2,'u'=>'minutes'],['t'=>'call','task'=>'','first'=>''],['t'=>'tag','v'=>'ai-called']]],
    'noreply'=>['name'=>'No-reply follow-up sequence','desc'=>'Contacted but silent for 3 days? Nudge on WhatsApp, then email, then an AI call.','i'=>'clock','c'=>'o','trig'=>'no_reply','tv'=>'3','steps'=>[['t'=>'whatsapp','text'=>"Hi {{first_name}}, just checking if you had a chance to look at the report we shared? Happy to explain it in a quick 15-minute call."],['t'=>'wait','n'=>2,'u'=>'days'],['t'=>'email','subject'=>'Quick follow-up – {{business_name}}','body'=>"Hi {{first_name}},\n\nI wanted to follow up on the audit we sent. You can pick a time for a free call here: {{booking_link}}\n\nRegards,\n{{sender_name}}",'attach'=>0],['t'=>'wait','n'=>3,'u'=>'days'],['t'=>'call','task'=>'','first'=>''],['t'=>'tag','v'=>'follow-up-done']]],
    'book'=>['name'=>'Qualified → book a meeting','desc'=>'When a lead becomes Qualified, send the booking link and remind once.','i'=>'cal','c'=>'t','trig'=>'status_changed','tv'=>'qualified','steps'=>[['t'=>'booking','ch'=>'whatsapp','text'=>"Hi {{first_name}}, great speaking with you! Please pick a convenient time for our strategy call:"],['t'=>'wait','n'=>1,'u'=>'days'],['t'=>'booking','ch'=>'email','subject'=>'Pick a time for your strategy call','text'=>"Hi {{first_name}},\n\nHere is the link to choose a time that suits you:"]]],
    'booked'=>['name'=>'Meeting booked → confirm & prepare','desc'=>'Confirm on WhatsApp, notify the team and run an audit before the meeting.','i'=>'check','c'=>'b','trig'=>'appointment_booked','steps'=>[['t'=>'whatsapp','text'=>"Thanks {{first_name}}! Your meeting with {{agency_name}} is confirmed. We'll share the meeting link before the call."],['t'=>'notify','subject'=>'Meeting booked: {{contact_name}}','text'=>'Meeting booked by {{contact_name}} ({{business_name}}) – {{phone}}'],['t'=>'report','contact'=>0]]],
    'report'=>['name'=>'Report sent → follow up','desc'=>'One day after the audit report, check in on WhatsApp; call if still silent.','i'=>'file','c'=>'y','trig'=>'report_ready','steps'=>[['t'=>'wait','n'=>1,'u'=>'days'],['t'=>'whatsapp','text'=>"Hi {{business_name}} team, did you get a chance to see the audit report? The quick wins on page 2 can bring more calls this month."],['t'=>'wait','n'=>2,'u'=>'days'],['t'=>'call','task'=>'','first'=>'']]]];}
  static function conds(){return ['source'=>'Source','status'=>'Status','city'=>'City','service'=>'Service','tags'=>'Tags','email'=>'Email','phone'=>'Phone','website'=>'Website','category'=>'Category'];}
  static function fire($trig,$lid,$ctx=[]){
    try{
      foreach(DB::all('SELECT * FROM automations WHERE active=1 AND trig=?',[$trig]) as $a){
        if($a['trig_value']!==''){
          $tv=strtolower($a['trig_value']);
          if($trig==='lead_created'&&strtolower((string)DB::val('SELECT source FROM leads WHERE id=?',[$lid]))!==$tv)continue;
          if($trig==='status_changed'&&strtolower($ctx['status']??'')!==$tv)continue;
          if($trig==='tag_added'&&strtolower($ctx['tag']??'')!==$tv)continue;}
        self::start($a,$lid);}}
    catch(Throwable $e){logmsg('Automation fire '.$trig.': '.$e->getMessage());}}
  static function start($a,$lid,$force=false){
    $l=DB::row('SELECT * FROM leads WHERE id=?',[$lid]);
    if(!$l||!self::match($a,$l))return false;
    if(!$force&&DB::val("SELECT id FROM automation_runs WHERE automation_id=? AND lead_id=? AND (status IN ('running','waiting') OR ? IN ('lead_created','report_ready','no_reply'))",[$a['id'],$lid,$a['trig']]))return false;
    $rid=DB::insert('automation_runs',['automation_id'=>$a['id'],'lead_id'=>$lid,'status'=>'running','log'=>'']);
    DB::q('UPDATE automations SET runs=runs+1 WHERE id=?',[$a['id']]);
    event($lid,'auto','Automation started: '.$a['name'],'auto');
    Queue::push('auto',$rid);
    return $rid;}
  static function match($a,$l){
    foreach(jd($a['conditions']) as $c){
      $f=$c['f']??'';$op=$c['op']??'is';$v=strtolower(trim($c['v']??''));
      $val=strtolower(trim((string)(strpos($f,'custom.')===0?(jd($l['custom'])[substr($f,7)]??''):($l[$f]??''))));
      $ok=match($op){'is'=>$val===$v,'not'=>$val!==$v,'contains'=>$v!==''&&strpos($val,$v)!==false,'empty'=>$val==='','not_empty'=>$val!=='',default=>true};
      if(!$ok)return false;}
    return true;}
  static function step($rid){
    $r=DB::row('SELECT r.*,a.steps,a.name,a.active,a.stop_on_reply FROM automation_runs r JOIN automations a ON a.id=r.automation_id WHERE r.id=?',[$rid]);
    if(!$r||!in_array($r['status'],['running','waiting'],true))return;
    if(!$r['active']){self::end($rid,'cancelled','Automation turned off');return;}
    $steps=jd($r['steps']);$i=(int)$r['step'];
    for($guard=0;$guard<15;$guard++){
      $l=DB::row('SELECT l.*,COALESCE(c.keyword,l.category) keyword FROM leads l LEFT JOIN campaigns c ON c.id=l.campaign_id WHERE l.id=?',[$r['lead_id']]);
      if(!$l){self::end($rid,'cancelled','Lead deleted');return;}
      if($r['stop_on_reply']&&$l['replied_at']&&strtotime($l['replied_at'])>=strtotime($r['created_at'])){self::end($rid,'stopped','Stopped – lead replied');return;}
      if(!isset($steps[$i])){self::end($rid,'done','Completed');event($l['id'],'auto','Automation finished: '.$r['name'],'auto');return;}
      $s=$steps[$i];$i++;
      $res=self::run($s,$l);
      self::log($rid,$i,$res['msg']);
      if(!empty($res['wait'])){DB::update('automation_runs',['step'=>$i,'status'=>'waiting','next_at'=>date('Y-m-d H:i:s',$res['wait'])],'id=?',[$rid]);Queue::push('auto',$rid,[],date('Y-m-d H:i:s',$res['wait']));return;}
      DB::update('automation_runs',['step'=>$i,'status'=>'running'],'id=?',[$rid]);}
    Queue::push('auto',$rid);}
  private static function run($s,$l){
    $vars=lead_vars($l);$t=$s['t']??'';
    switch($t){
      case 'wait':$sec=max(1,(int)($s['n']??1))*['minutes'=>60,'hours'=>3600,'days'=>86400][$s['u']??'hours'];return ['wait'=>time()+$sec,'msg'=>'Waiting '.(int)$s['n'].' '.($s['u']??'hours')];
      case 'whatsapp':case 'booking':
        if(!$l['phone']&&!($t==='booking'&&($s['ch']??'')==='email'))return ['msg'=>'Skipped WhatsApp – no phone number'];
        $text=tpl($s['text']??'',$vars);
        if($t==='booking'&&strpos($text,$vars['booking_link'])===false)$text.="\n\n".$vars['booking_link'];
        if($t==='booking'&&($s['ch']??'whatsapp')==='email'){
          if(!$l['email']||!Mailer::ready())return ['msg'=>'Skipped booking email – no email address or email not set up'];
          Outreach::email($l['id'],$l['email'],tpl($s['subject']??'Book a time with {{agency_name}}',$vars),$text);
          event($l['id'],'sent','Booking link sent by email','email');return ['msg'=>'Booking link emailed'];}
        $files=!empty($s['attach'])&&DB::val('SELECT id FROM reports WHERE lead_id=?',[$l['id']])?Outreach::files($l):[];
        $id=WhatsApp::send($l['phone'],$text,$files,$vars);
        if($id==='manual'){DB::update('leads',['wa_status'=>'queued'],'id=?',[$l['id']]);event($l['id'],'wa_manual','WhatsApp ready to send manually: '.mb_strimwidth($text,0,120,'…'),'whatsapp');return ['msg'=>'WhatsApp prepared (manual mode)'];}
        DB::insert('messages',['lead_id'=>$l['id'],'channel'=>'whatsapp','status'=>'sent','ext_id'=>mb_substr($id,0,190),'body'=>$text]);
        DB::update('leads',['wa_status'=>'sent'],'id=?',[$l['id']]);
        event($l['id'],'sent',($t==='booking'?'Booking link':'WhatsApp message').' sent by automation','whatsapp');return ['msg'=>'WhatsApp sent'];
      case 'email':
        if(!$l['email'])return ['msg'=>'Skipped email – no email address'];
        if(!Mailer::ready())throw new Exception('Email is not set up (Settings → Messaging → Email)');
        $sub=tpl($s['subject']??'',$vars);$text=tpl($s['body']??'',$vars);
        $att=[];if(!empty($s['attach'])&&DB::val('SELECT id FROM reports WHERE lead_id=?',[$l['id']]))foreach(Outreach::files($l) as $f)$att[$f['name']]=$f['path'];
        Outreach::email($l['id'],$l['email'],$sub,$text,$att);
        if(!in_array($l['email_status'],['sent','opened'],true))DB::update('leads',['email_status'=>'sent'],'id=?',[$l['id']]);
        event($l['id'],'sent','Email sent by automation: '.$sub,'email');return ['msg'=>'Email sent'];
      case 'call':
        if(!$l['phone'])return ['msg'=>'Skipped call – no phone number'];
        $cid=Voice::start($l['id'],['task'=>$s['task']??'','first'=>$s['first']??'']);return ['msg'=>'AI call started (#'.$cid.')'];
      case 'status':CRM::setStatus($l['id'],$s['v']??'new','by automation');return ['msg'=>'Status → '.($s['v']??'')];
      case 'tag':CRM::addTag($l['id'],$s['v']??'');return ['msg'=>'Tag '.($s['v']??'')];
      case 'assign':DB::update('leads',['assigned_to'=>(int)($s['v']??0)?:null],'id=?',[$l['id']]);event($l['id'],'note','Assigned to '.(DB::val('SELECT name FROM users WHERE id=?',[(int)($s['v']??0)])?:'nobody'));return ['msg'=>'Assigned'];
      case 'followup':$at=date('Y-m-d H:i:s',time()+max(1,(int)($s['n']??1))*['hours'=>3600,'days'=>86400][$s['u']??'days']);DB::update('leads',['follow_up_at'=>$at],'id=?',[$l['id']]);event($l['id'],'note','Follow-up set for '.date('d M, h:i A',strtotime($at)));return ['msg'=>'Follow-up '.$at];
      case 'notify':
        $to=$s['to']??''?:setting('company_email');
        if(!Mailer::ready()||!$to)return ['msg'=>'Skipped team email – email not set up'];
        $text=tpl($s['text']??'New lead: {{business_name}} – {{phone}} {{email}}',$vars)."\n\n".base_url('index.php?r=lead&id='.$l['id']);Mailer::send($to,'Setu: '.tpl($s['subject']??'Lead update – {{business_name}}',$vars),Mailer::html($text),$text);
        return ['msg'=>'Team notified ('.$to.')'];
      case 'report':Outreach::initiate([$l['id']],['auto'=>!empty($s['contact'])]);return ['msg'=>'Audit & report started'];
      case 'webhook':
        $u=$s['url']??'';if(!preg_match('~^https?://~',$u))return ['msg'=>'Skipped webhook – invalid URL'];
        $data=array_intersect_key($l,array_flip(['id','name','contact_name','email','phone','city','website','source','status','tags','service','deal_value']))+['custom'=>jd($l['custom']),'link'=>base_url('index.php?r=lead&id='.$l['id'])];
        $r=http($u,['json'=>$data,'timeout'=>15]);return ['msg'=>'Webhook HTTP '.$r['code']];}
    return ['msg'=>'Unknown step '.$t];}
  private static function log($rid,$i,$m){DB::q("UPDATE automation_runs SET log=CONCAT(COALESCE(log,''),?) WHERE id=?",['['.date('d M H:i').'] Step '.$i.': '.$m."\n",$rid]);}
  static function end($rid,$st,$m){DB::update('automation_runs',['status'=>$st,'next_at'=>null],'id=?',[$rid]);self::log($rid,'end',$m);}
  static function failed($rid,$msg){
    $r=DB::row('SELECT r.lead_id,a.name FROM automation_runs r JOIN automations a ON a.id=r.automation_id WHERE r.id=?',[$rid]);
    self::end($rid,'failed','Error: '.$msg);
    if($r)event($r['lead_id'],'error','Automation "'.$r['name'].'" failed: '.$msg,'auto');}
  static function onReply($lid){
    foreach(DB::all("SELECT r.id FROM automation_runs r JOIN automations a ON a.id=r.automation_id WHERE r.lead_id=? AND a.stop_on_reply=1 AND r.status IN ('running','waiting')",[$lid]) as $r){
      DB::q("UPDATE jobs SET status='cancelled' WHERE type='auto' AND ref_id=? AND status='pending'",[$r['id']]);
      self::end($r['id'],'stopped','Stopped – lead replied');}}
  static function cancel($rid){DB::q("UPDATE jobs SET status='cancelled' WHERE type='auto' AND ref_id=? AND status='pending'",[$rid]);self::end($rid,'cancelled','Cancelled manually');}
  // Time-based triggers, called from the scheduler
  static function scan(){
    foreach(DB::all("SELECT * FROM automations WHERE active=1 AND trig IN ('no_reply','follow_up_due')") as $a){
      if($a['trig']==='no_reply'){
        $d=max(1,(int)$a['trig_value']);
        $rows=DB::all("SELECT l.id FROM leads l WHERE l.replied_at IS NULL AND l.stage NOT IN ('won','lost') AND l.status NOT IN ('won','lost') AND EXISTS(SELECT 1 FROM messages m WHERE m.lead_id=l.id AND m.direction='out' AND m.created_at<NOW()-INTERVAL $d DAY) AND NOT EXISTS(SELECT 1 FROM automation_runs r WHERE r.lead_id=l.id AND r.automation_id=?) LIMIT 50",[$a['id']]);}
      else $rows=DB::all("SELECT l.id FROM leads l WHERE l.follow_up_at IS NOT NULL AND l.follow_up_at<=NOW() AND l.follow_up_at>NOW()-INTERVAL 2 DAY AND NOT EXISTS(SELECT 1 FROM automation_runs r WHERE r.lead_id=l.id AND r.automation_id=? AND r.created_at>=l.follow_up_at) LIMIT 50",[$a['id']]);
      foreach($rows as $r)self::start($a,$r['id'],true);}}}
