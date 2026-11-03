<?php
final class Outreach{
  // Start audit → report → (optional) contact for the given leads
  static function initiate($ids,$o){
    [$in,$v]=DB::in($ids);$n=0;
    // Several reports at once → their AI writing goes out as one batch (faster overall, half price, no long server waits)
    $bulk=count($v)>=max(2,(int)setting('batch_min','4'));
    foreach(DB::all("SELECT id,stage FROM leads WHERE id IN ($in)",$v) as $l){
      if(in_array($l['stage'],['queued','auditing','reporting','contacting'],true))continue;
      DB::q("DELETE FROM jobs WHERE ref_id=? AND type IN ('audit','report','contact') AND status='pending'",[$l['id']]);
      DB::update('leads',['abort_flag'=>0,'progress'=>1,'progress_msg'=>'Waiting in queue','last_error'=>null],'id=?',[$l['id']]);
      Queue::push('audit',$l['id'],['auto'=>!empty($o['auto']),'pw'=>(int)($o['pw']??0),'pn'=>(int)($o['pn']??0),'bulk'=>$bulk]);
      stage($l['id'],'queued','Process initiated'.(!empty($o['auto'])?' (auto-contact on)':''));$n++;}
    return $n;}
  static function bulk($a,$ids){
    if(!$ids)return 'Select at least one lead.';
    if($a==='initiate')return self::initiate($ids,['auto'=>!empty($_POST['auto']),'pw'=>$_POST['pw']??0,'pn'=>$_POST['pn']??0]).' lead(s) queued for audit & report.';
    [$in,$v]=DB::in($ids);
    if($a==='contact'){$n=0;foreach(DB::all("SELECT l.id FROM leads l WHERE l.id IN ($in) AND EXISTS(SELECT 1 FROM reports r WHERE r.lead_id=l.id)",$v) as $l){self::schedule($l['id']);$n++;}return $n.' lead(s) scheduled for outreach'.($n<count($ids)?' (leads without a report were skipped)':'').'.';}
    if($a==='won'||$a==='lost'){foreach($ids as $i){stage($i,$a,'Marked as '.($a==='won'?'Won':'Not interested'));if($a==='won')self::promote($i,'won');else DB::update('leads',['status'=>'lost'],'id=? AND is_lead=1',[$i]);}return count($ids).' record(s) updated.';}
    if($a==='promote'){foreach($ids as $i)self::promote($i);return count($ids).' record(s) moved to Leads.';}
    if($a==='abort')return Queue::abort($ids).' process(es) stopped.';
    if(strpos($a,'status:')===0){$st=substr($a,7);if(isset(crm_statuses()[$st]))foreach($ids as $i)CRM::setStatus($i,$st);return count($ids).' lead(s) updated.';}
    if($a==='delete'){foreach(DB::all("SELECT pdf FROM reports WHERE lead_id IN ($in)",$v) as $r)@unlink(ROOT.'/'.$r['pdf']);foreach(['reports','events','messages'] as $t)DB::q("DELETE FROM $t WHERE lead_id IN ($in)",$v);DB::q("DELETE FROM jobs WHERE type NOT IN ('search','auto') AND ref_id IN ($in)",$v);foreach(['appointments','calls','automation_runs'] as $t)DB::q("DELETE FROM $t WHERE lead_id IN ($in)",$v);DB::q("DELETE FROM leads WHERE id IN ($in)",$v);return count($ids).' lead(s) deleted.';}
    return 'Unknown action';}
  static function csv($rows){
    header('Content-Type: text/csv; charset=utf-8');header('Content-Disposition: attachment; filename="setu-'.date('Ymd-His').'.csv"');
    $o=fopen('php://output','w');fwrite($o,"\xEF\xBB\xBF");
    // Cells starting with = + - @ would run as formulas in Excel/Sheets
    $cl=fn($r)=>array_map(fn($v)=>is_string($v)&&$v!==''&&strpbrk($v[0],"=+-@\t\r")!==false&&!is_numeric($v)?"'".$v:$v,$r);
    $fs=DB::all('SELECT fkey,label FROM fields ORDER BY sort,id');
    fputcsv($o,array_merge(['Name','Contact person','Category','Address','City','Phone','Alt phone','Email','Website','Source','Source detail','Status','Service','Deal value','Tags','Follow-up','Rating','Reviews','Maps URL','Website score','Maps score','Outreach stage','Email status','WhatsApp status','Replied at','Created'],array_column($fs,'label')));
    foreach($rows as $l){$c=jd($l['custom']);fputcsv($o,$cl(array_merge([$l['name'],$l['contact_name'],$l['category'],$l['address'],$l['city'],$l['phone'],$l['alt_phone'],$l['email'],$l['website'],$l['source'],$l['source_detail'],crm_statuses()[$l['status']][0]??$l['status'],$l['service'],$l['deal_value'],$l['tags'],$l['follow_up_at'],$l['rating'],$l['reviews'],$l['maps_url'],$l['web_score'],$l['map_score'],stages()[$l['stage']][0]??$l['stage'],$l['email_status'],$l['wa_status'],$l['replied_at'],$l['created_at']],array_map(fn($f)=>is_array($c[$f['fkey']]??'')?implode(', ',$c[$f['fkey']]):($c[$f['fkey']]??''),$fs))));}
    exit;}
  static function schedule($lid,$now=false){
    $gap=max(0,(int)setting('send_gap','90'));
    $last=DB::val("SELECT MAX(run_after) FROM jobs WHERE type='contact' AND status IN ('pending','running')");
    $t=max(time(),$last?strtotime($last)+$gap:0);
    if($now)$t=time();
    DB::q("DELETE FROM jobs WHERE type='contact' AND ref_id=? AND status='pending'",[$lid]);
    Queue::push('contact',$lid,[],date('Y-m-d H:i:s',$t));
    event($lid,'scheduled','Outreach scheduled for '.date('d M, h:i A',$t));}
  static function message($type,$lead){
    $p=DB::row('SELECT * FROM prompts WHERE type=? ORDER BY is_active DESC,id LIMIT 1',[$type]);
    return $p?[$p['subject'],$p['content']]:['',''];}
  static function files($lead){
    $r=DB::row('SELECT * FROM reports WHERE lead_id=? ORDER BY id DESC LIMIT 1',[$lead['id']]);
    if(!$r)throw new Exception('No report generated yet');
    $slug=preg_replace('/[^a-z0-9]+/i','-',mb_substr($lead['name'],0,40));
    $f=[];
    if(report_mode()!=='link'){$pf=Report::pdfFile($r);if(!$pf)throw new Exception('Report PDF could not be created - regenerate the report');$f[]=['path'=>$pf,'url'=>base_url('report.php?t='.$r['token'].'&dl=1'),'name'=>'Audit-Report-'.$slug.'.pdf'];}
    if(!$lead['website']&&($pp=setting('proposal_pdf'))&&is_file(ROOT.'/'.$pp))$f[]=['path'=>ROOT.'/'.$pp,'url'=>base_url('report.php?proposal='.$r['token']),'name'=>'Website-Proposal-'.preg_replace('/[^a-z0-9]+/i','-',setting('company_name','Agency')).'.pdf'];
    return $f;}
  static function contact($lid,$p=[]){
    $l=DB::row('SELECT l.*,COALESCE(c.keyword,l.category) keyword FROM leads l LEFT JOIN campaigns c ON c.id=l.campaign_id WHERE l.id=?',[$lid]);
    if(!$l)return;
    if($l['abort_flag'])throw new Aborted('Stopped by user');
    $limit=(int)setting('daily_limit','60');
    if($limit&&empty($p['force'])&&(int)DB::val("SELECT COUNT(DISTINCT lead_id) FROM messages WHERE direction='out' AND DATE(created_at)=CURDATE()")>=$limit){
      Queue::push('contact',$lid,$p,date('Y-m-d 10:00:00',strtotime('+1 day')));
      event($lid,'scheduled','Daily sending limit reached – moved to tomorrow');return;}
    if(setting('skip_contacted')==='1'&&empty($p['force'])&&$l['source']==='maps'&&($o=DB::row("SELECT id,campaign_id FROM leads WHERE place_id=? AND id<>? AND (email_status IN ('sent','opened') OR wa_status IN ('sent','delivered','read','manual')) LIMIT 1",[$l['place_id'],$lid]))){
      stage($lid,'skipped','Skipped – already contacted in campaign #'.$o['campaign_id']);return;}
    stage($lid,'contacting');
    Queue::progress($lid,95,'Sending email & WhatsApp');
    $files=self::files($l);
    $vars=lead_vars($l);
    $nw=$l['website']?'':'_noweb';
    $ok=0;$err=[];
    $ch=$p['channels']??['email','whatsapp'];
    if(in_array('email',$ch,true)&&setting('send_email')==='1'&&$l['email']&&$l['email_status']!=='sent'){
      if(!Mailer::ready()){$err[]='Email: not set up';event($lid,'error','Email not sent – set up email in Settings → Email','email');}
      else try{
        [$sub,$body]=self::message('email'.$nw,$l);
        if(report_mode()!=='pdf'&&strpos($body,'{{report_link}}')===false)$body.="\n\nYour report: {{report_link}}";
        $sub=tpl($sub,$vars);$text=tpl($body,$vars);
        $att=[];foreach($files as $f)$att[$f['name']]=$f['path'];
        self::email($lid,$l['email'],$sub,$text,$att);
        DB::update('leads',['email_status'=>'sent'],'id=?',[$lid]);
        event($lid,'sent','Email sent to '.$l['email'].($att?' with '.count($att).' PDF attachment(s)':' with the report link'),'email');$ok++;}
      catch(Throwable $e){DB::update('leads',['email_status'=>'failed'],'id=?',[$lid]);event($lid,'error','Email failed: '.$e->getMessage(),'email');$err[]='Email: '.$e->getMessage();}}
    if(in_array('whatsapp',$ch,true)&&setting('send_whatsapp')==='1'&&$l['phone']&&!in_array($l['wa_status'],['sent','delivered','read'],true)){
      [,$body]=self::message('whatsapp'.$nw,$l);
      if(report_mode()!=='pdf'&&strpos($body,'{{report_link}}')===false)$body.="\n\nYour report: {{report_link}}";
      $text=tpl($body,$vars);
      try{
        $id=WhatsApp::send($l['phone'],$text,$files,$vars);
        if($id==='manual'){DB::update('leads',['wa_status'=>'queued'],'id=?',[$lid]);event($lid,'wa_manual','WhatsApp message ready – click "Send on WhatsApp" on the lead to send it manually','whatsapp');}
        else{DB::insert('messages',['lead_id'=>$lid,'channel'=>'whatsapp','status'=>'sent','ext_id'=>mb_substr($id,0,190),'body'=>$text]);DB::update('leads',['wa_status'=>'sent'],'id=?',[$lid]);event($lid,'sent','WhatsApp message + PDF sent to '.phone_norm($l['phone']),'whatsapp');$ok++;}}
      catch(Throwable $e){DB::update('leads',['wa_status'=>'failed'],'id=?',[$lid]);event($lid,'error','WhatsApp failed: '.$e->getMessage(),'whatsapp');$err[]='WhatsApp: '.$e->getMessage();}}
    DB::update('leads',['progress'=>100,'progress_msg'=>$ok?'Sent':'Done'],'id=?',[$lid]);
    $l2=DB::row('SELECT email_status,wa_status FROM leads WHERE id=?',[$lid]);
    if($ok||in_array($l2['email_status'],['sent','opened'],true)||in_array($l2['wa_status'],['sent','delivered','read','manual'],true))stage($lid,'contacted');
    elseif($l2['wa_status']==='queued')stage($lid,'report_ready');
    elseif($err)throw new Exception(implode(' | ',$err));
    else stage($lid,'report_ready','No email or WhatsApp number available to contact');}
  // Send an email to a lead and keep it in the lead's conversation (Emails tab). $reply = message row being answered.
  static function email($lid,$to,$sub,$text,$att=[],$reply=null){
    $tk=token(12);$dom=substr(strrchr(Mailer::from()?:'setu@localhost','@'),1)?:'localhost';$mid='<setu.'.$tk.'@'.$dom.'>';$hd=[];
    if($reply){$ref=trim(($reply['in_reply_to']?:'').' '.($reply['msg_id']?:''));if($reply['msg_id']){$hd['In-Reply-To']=$reply['msg_id'];$hd['References']=trim($ref);}}
    $sent=Mailer::send($to,$sub,Mailer::html($text,base_url('track.php?t='.$tk)),$text,$att,['msg_id'=>$mid,'headers'=>$hd]);
    // SMTP/PHP mail use our Message-ID; services return their own when they share it
    $msgId=Mailer::mode()==='api'?(preg_match('/^<[^>]+@[^>]+>$/',(string)$sent)?$sent:''):$mid;
    return DB::insert('messages',['lead_id'=>$lid,'channel'=>'email','direction'=>'out','status'=>'sent','ext_id'=>mb_substr((string)$sent,0,190),'msg_id'=>mb_substr($msgId,0,250),'in_reply_to'=>mb_substr((string)($reply['msg_id']??''),0,250),'addr'=>mb_substr(strtolower($to),0,190),'subject'=>mb_substr($sub,0,250),'body'=>$text,'token'=>$tk]);}
  // Pull new emails from the inbox; replies from leads land in their conversation (Emails tab)
  static function syncInbox(){
    if(!Mailer::imapOn())return 0;
    $n=0;
    foreach(Mailer::fetchNew() as $m)if(self::store($m))$n++;
    Settings::set('imap_last',time());
    return $n;}
  // Save one received email in the matching lead's conversation; returns the lead id (0 = not from a lead, ignored)
  static function store($m){
    $mine=strtolower(Mailer::from());
    if($m['from']===''||$m['from']===$mine||$m['auto']&&!$m['in_reply_to'])return 0;
    if($m['msg_id']!==''&&DB::val("SELECT id FROM messages WHERE channel='email' AND direction='in' AND msg_id=?",[$m['msg_id']]))return 0;
    // Which lead: the conversation it answers, else the sender's address
    $lid=0;$ids=array_values(array_filter(preg_split('/\s+/',$m['in_reply_to'].' '.$m['references'])));
    if($ids){$in=implode(',',array_fill(0,count($ids),'?'));$lid=(int)DB::val("SELECT lead_id FROM messages WHERE channel='email' AND msg_id IN ($in) ORDER BY id DESC LIMIT 1",$ids);}
    if(!$lid)$lid=(int)DB::val('SELECT id FROM leads WHERE LOWER(email)=? ORDER BY is_lead DESC,last_activity DESC LIMIT 1',[$m['from']]);
    if(!$lid)return 0;
    DB::insert('messages',['lead_id'=>$lid,'channel'=>'email','direction'=>'in','status'=>'received','msg_id'=>mb_substr($m['msg_id'],0,250),'in_reply_to'=>mb_substr($m['in_reply_to'],0,250),'addr'=>mb_substr($m['from'],0,190),'subject'=>mb_substr($m['subject'],0,250),'body'=>$m['text'],'is_read'=>0,'created_at'=>$m['date']]);
    $was=DB::val('SELECT replied_at FROM leads WHERE id=?',[$lid]);
    self::markReplied($lid,'email',$m['text']);
    if($was)event($lid,'replied','New email: '.mb_strimwidth($m['subject'],0,120,'…'),'email');
    return $lid;}
  static function markReplied($lid,$via,$msg=''){
    $l=DB::row('SELECT replied_at FROM leads WHERE id=?',[$lid]);
    if(!$l||$l['replied_at'])return;
    DB::update('leads',['replied_at'=>date('Y-m-d H:i:s'),'replied_via'=>$via,'stage'=>'replied'],'id=?',[$lid]);
    event($lid,'replied','Replied via '.$via.($msg?': '.mb_substr($msg,0,300):''),$via);
    if(setting('promote_on_reply')==='1')self::promote($lid,'contacted');
    Automation::onReply($lid);
    Automation::fire('replied',$lid);}
  // Move a scraped business into the CRM Leads list
  static function promote($lid,$status='new'){
    $l=DB::row('SELECT is_lead,status FROM leads WHERE id=?',[$lid]);
    if(!$l)return;
    if($l['is_lead']){if($status==='won'&&$l['status']!=='won')CRM::setStatus($lid,'won');return;}
    DB::update('leads',['is_lead'=>1,'status'=>$status,'lead_at'=>date('Y-m-d H:i:s')],'id=?',[$lid]);
    event($lid,'stage','Moved to Leads ('.(crm_statuses()[$status][0]??$status).')');
    Automation::fire('lead_created',$lid);}
  static function checkReplies(){return self::syncInbox();}}
