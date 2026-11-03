<?php
final class Queue{
  const LEAD_JOBS="'enrich','audit','report','contact'";
  static function push($type,$ref,$payload=[],$at=null){
    return DB::insert('jobs',['type'=>$type,'ref_id'=>(int)$ref,'payload'=>je($payload),'run_after'=>$at?:date('Y-m-d H:i:s')]);}
  static function due(){return (int)DB::val("SELECT COUNT(*) FROM jobs WHERE status='pending' AND run_after<=NOW()");}
  static function dueLong(){return (int)DB::val("SELECT COUNT(*) FROM jobs WHERE status='pending' AND run_after<=NOW() AND type IN ('audit','report','prices','aipar')");}
  // Progress shown on the lead row and in the activity panel
  static function progress($lid,$pct,$msg){
    // (the AI call itself is logged as its own step with tokens and cost)
    if(Trace::$lead===(int)$lid&&$msg!==Trace::$msg&&strncmp($msg,'AI is ',6))Trace::step($msg);
    DB::update('leads',['progress'=>max(0,min(100,(int)$pct)),'progress_msg'=>mb_substr($msg,0,180)],'id=?',[$lid]);
    if((int)DB::val('SELECT abort_flag FROM leads WHERE id=?',[$lid]))throw new Aborted('Stopped by user');}
  // Stop everything for these leads: pending jobs are cancelled, a running job stops at its next step
  static function abort($ids){
    [$in,$v]=DB::in($ids);if(!$v)return 0;
    DB::q("UPDATE ai_batch SET status='cancelled' WHERE status IN ('queued','sent') AND lead_id IN ($in)",$v);
    DB::q("UPDATE jobs SET status='cancelled',error='Stopped by user' WHERE status='pending' AND type IN (".self::LEAD_JOBS.") AND ref_id IN ($in)",$v);
    DB::q("UPDATE jobs SET status='cancelled',error='Stopped by user' WHERE status='running' AND type IN (".self::LEAD_JOBS.") AND ref_id IN ($in)",$v);
    $n=0;
    foreach(DB::all("SELECT id,stage FROM leads WHERE id IN ($in)",$v) as $l){
      if(!in_array($l['stage'],['queued','auditing','reporting','contacting'],true))continue;
      $back=DB::val('SELECT COUNT(*) FROM reports WHERE lead_id=?',[$l['id']])?'report_ready':'new';
      DB::update('leads',['abort_flag'=>1,'stage'=>$back,'progress'=>0,'progress_msg'=>'Stopped'],'id=?',[$l['id']]);
      event($l['id'],'error','Process stopped manually');$n++;}
    return $n;}
  // Heading of a job in "View process"
  static function title($j){$p=jd($j['payload']);$a=(int)$j['attempts']>1?' (attempt '.(int)$j['attempts'].')':'';
    if($j['type']==='audit')return 'Audit started'.$a;
    if($j['type']==='contact')return 'Sending the report'.$a;
    return ['research'=>'Report · step 1 of 3: market research','write'=>'Report · step 2 of 3: AI writes the report','save'=>'Report · step 2 of 3: saving the AI answer','pdf'=>'Report · step 3 of 3: PDF design'][$p['phase']??'research'].$a;}
  const LONG=['audit','report','prices','aipar'];
  // $quick=true: only short jobs (searches, contact scans, sends, automations) – used by the browser poll,
  // so slow AI steps always run in the detached background worker where web-server time limits do not apply
  static function work($budget=null,$quick=false){
    $budget=$budget?:(int)setting('queue_budget','20');
    @set_time_limit(max(300,$budget+280));
    @ignore_user_abort(true);
    $t0=microtime(true);$n=0;
    self::reapStuck();
    while(microtime(true)-$t0<$budget){
      $j=DB::row("SELECT * FROM jobs WHERE status='pending' AND run_after<=NOW()".($quick?" AND type NOT IN ('audit','report','prices','aipar')":'')." ORDER BY FIELD(type,'search','enrich','auto','audit','report','contact','prices','batch','aipar'),id LIMIT 1");
      if(!$j)break;
      if(!DB::q("UPDATE jobs SET status='running',locked_at=NOW(),attempts=attempts+1 WHERE id=? AND status='pending'",[$j['id']])->rowCount())continue;
      $tr=in_array($j['type'],['audit','report','contact'],true);
      if($tr)Trace::begin($j['ref_id'],$j['id'],self::title($j));
      try{self::handle($j);DB::q("UPDATE jobs SET status='done',error=NULL WHERE id=? AND status='running'",[$j['id']]);if($tr)Trace::end();}
      catch(Defer $e){DB::q("UPDATE jobs SET status='pending',locked_at=NULL,attempts=GREATEST(attempts-1,0),run_after=NOW()+INTERVAL ? SECOND WHERE id=?",[max(5,(int)$e->getCode()),$j['id']]);if($tr)Trace::end('ok',$e->getMessage().' – trying again in '.max(5,(int)$e->getCode()).'s');}
      catch(Aborted $e){DB::q("UPDATE jobs SET status='cancelled',error=? WHERE id=?",[$e->getMessage(),$j['id']]);if($tr){Trace::step('Stopped by user','step');Trace::end('err','The process was aborted.');}}
      catch(Throwable $e){if($tr){Trace::close('err',$e->getMessage());Trace::step(((int)$j['attempts']<2&&!in_array($j['type'],['search','auto'],true)?'Error – will retry in 1 minute':'Failed').': '.mb_strimwidth($e->getMessage(),0,140,'…'),'step');Trace::end('err',$e->getMessage());}self::fail($j,$e);}
      $n++;}
    return $n;}
  // Jobs killed by the host (timeout) stay "running": retry them, then fail with a clear message
  static function reapStuck(){
    $min=max(3,(int)setting('job_timeout','6'));
    foreach(DB::all("SELECT * FROM jobs WHERE status='running' AND locked_at<NOW()-INTERVAL IF(type IN ('report','aipar'),GREATEST(?,8),?) MINUTE",[$min,$min]) as $j){
      $msg='Timed out – the server stopped this step after '.$min.' min (often a slow AI model or a host time limit).';
      if(in_array($j['type'],['audit','report','contact'],true)){DB::q("UPDATE job_steps SET status='err',detail=? WHERE job_id=? AND status='run'",['The server stopped this step (time limit or crash).',$j['id']]);DB::insert('job_steps',['lead_id'=>$j['ref_id'],'job_id'=>$j['id'],'kind'=>'step','step'=>mb_substr($msg,0,190),'status'=>'err']);}
      if($j['attempts']<2&&!in_array($j['type'],['search','prices','aipar'],true)){DB::q("UPDATE jobs SET status='pending',locked_at=NULL,error=? WHERE id=?",[$msg.' Retrying…',$j['id']]);continue;}
      self::fail($j,new Exception($msg.' Try again, or choose a faster AI model in AI Connection.'),true);}}
  private static function fail($j,$e,$final=false){
    $msg=$e->getMessage();
    logmsg("Job {$j['id']} {$j['type']}#{$j['ref_id']}: $msg");
    $att=(int)$j['attempts'];
    if(!$final&&$att<2&&!in_array($j['type'],['search','auto'],true)){
      DB::q("UPDATE jobs SET status='pending',locked_at=NULL,error=?,run_after=NOW()+INTERVAL 1 MINUTE WHERE id=?",[$msg,$j['id']]);
      if($j['type']!=='enrich')DB::update('leads',['progress_msg'=>'Retrying after error: '.mb_substr($msg,0,140)],'id=?',[$j['ref_id']]);
      return;}
    DB::q("UPDATE jobs SET status='failed',error=? WHERE id=?",[$msg,$j['id']]);
    if($j['type']==='search')DB::update('campaigns',['status'=>'error','error'=>$msg],'id=?',[$j['ref_id']]);
    elseif($j['type']==='auto')Automation::failed($j['ref_id'],$msg);
    elseif(in_array($j['type'],['prices','batch','aipar'],true))return;
    elseif($j['type']!=='enrich'){DB::update('leads',['stage'=>'failed','last_error'=>$msg,'progress_msg'=>'Failed'],'id=?',[$j['ref_id']]);event($j['ref_id'],'error',ucfirst($j['type']).' failed: '.$msg);}}
  private static function handle($j){
    $p=jd($j['payload']);$id=(int)$j['ref_id'];
    switch($j['type']){
      case 'search':self::search($id,$p);break;
      case 'enrich':self::enrich($id);break;
      case 'audit':self::audit($id,$p);break;
      case 'report':self::report($id,$p);break;
      case 'contact':Outreach::contact($id,$p);break;
      case 'auto':Automation::step($id);break;
      case 'call':Voice::call($id,$p);break;
      case 'prices':Usage::aiPrices();break;
      case 'batch':Batch::tick();break;
      case 'aipar':Parallel::run($j['id']);break;}}
  // Map search, one result page per job. Businesses already in your data (any earlier search or Leads) are skipped.
  // Google Places: a free IDs-only request first, so pages with nothing new cost nothing and only new businesses are paid for.
  private static function search($cid,$p){
    $c=DB::row('SELECT * FROM campaigns WHERE id=?',[$cid]);
    if(!$c||$c['status']==='stopped')return;
    $q=trim($c['keyword'].($c['location']?' in '.$c['location']:''));
    $have=(int)$c['found'];$need=(int)$c['limit_n']-$have;$skip=(int)$c['skipped'];$next=$p['next']??null;
    if($need<=0){DB::update('campaigns',['status'=>'ready'],'id=?',[$cid]);return;}
    $dedupe=setting('maps_skip_existing','1')==='1';
    // place_id => 1 when already in this search, 0 when in another search / Leads
    $known=function($ids)use($cid,$dedupe){if(!$ids)return [];[$in,$v]=[implode(',',array_fill(0,count($ids),'?')),array_values($ids)];$o=[];foreach(DB::all("SELECT place_id,MAX(campaign_id=?) mine FROM leads WHERE place_id IN ($in) GROUP BY place_id",array_merge([$cid],$v)) as $r)if($r['mine']||$dedupe)$o[$r['place_id']]=(int)$r['mine'];return $o;};
    if($c['source']==='places'){
      $page=Maps::ids($q,$next);
      $kn=$known($page['ids']);
      foreach($page['ids'] as $id)if(isset($kn[$id])&&!$kn[$id])$skip++;
      $new=array_slice(array_values(array_diff($page['ids'],array_keys($kn))),0,$need);
      $items=[];
      if($new){
        // Cheapest way to fetch only the new ones: 1 Text Search page, or 1 Place Details request per business
        $P=Usage::prices();$pt=(float)($P[Maps::sku()]['price']??0.035);$pd=(float)($P[Maps::sku(true)]['price']??0.02);
        $full=null;
        if(count($new)*$pd>=$pt){try{$full=Maps::places($q,$next);}catch(Throwable $e){logmsg('Places page fetch, using details instead: '.$e->getMessage());}}
        if($full){$by=array_column($full['items'],null,'place_id');foreach($new as $id)if(isset($by[$id]))$items[]=$by[$id];}
        $miss=array_diff($new,array_column($items,'place_id'));
        foreach($miss as $id)$items[]=Maps::details($id);}
      $res=['items'=>$items,'next'=>$page['next'],'more'=>(bool)$page['ids']];}
    else{
      $r=Maps::search($q,$next,$c['source']);
      $kn=$known(array_column($r['items'],'place_id'));$items=[];
      foreach($r['items'] as $it){if(isset($kn[$it['place_id']])){if(!$kn[$it['place_id']])$skip++;continue;}$items[]=$it;}
      $res=['items'=>$items,'next'=>$r['next'],'more'=>(bool)$r['items']];}
    foreach($res['items'] as $it){
      if($have>=$c['limit_n'])break;
      if(DB::val('SELECT id FROM leads WHERE campaign_id=? AND place_id=?',[$cid,$it['place_id']]))continue;
      $have++;
      $lid=DB::insert('leads',['campaign_id'=>$cid,'position'=>$have,'place_id'=>$it['place_id'],'name'=>mb_substr($it['name'],0,250),'category'=>mb_substr($it['category'],0,180),'address'=>mb_substr($it['address'],0,490),'city'=>mb_substr($it['city']?:$c['location'],0,110),'phone'=>$it['phone'],'phone_source'=>$it['phone']?'maps':'','website'=>mb_substr($it['website'],0,490),'rating'=>$it['rating'],'reviews'=>(int)$it['reviews'],'maps_url'=>mb_substr($it['maps_url'],0,490),'lat'=>$it['lat'],'lng'=>$it['lng'],'raw'=>je($it['raw']),'source'=>'maps','source_detail'=>mb_substr($q,0,180),'token'=>token(16),'last_activity'=>date('Y-m-d H:i:s')]);
      event($lid,'found','Found on Google Maps at position #'.$have.' for "'.$q.'"','maps');
      if($it['website'])self::push('enrich',$lid);}
    $u=['found'=>$have,'skipped'=>$skip];
    if($have<$c['limit_n']&&!empty($res['next'])&&$res['more'])self::push('search',$cid,['next'=>$res['next']],date('Y-m-d H:i:s',time()+($c['source']==='places'?2:0)));
    else $u['status']='ready';
    DB::update('campaigns',$u,'id=?',[$cid]);}
  static function enrich($lid){
    $l=DB::row('SELECT * FROM leads WHERE id=?',[$lid]);
    if(!$l||!$l['website'])return;
    $c=Scraper::contacts($l['website']);
    $u=['socials'=>je($c['socials']+($c['whatsapp']?['whatsapp'=>$c['whatsapp']]:[]))];
    $found=[];
    if(!$l['phone']&&$c['phones']){$u['phone']=mb_substr($c['phones'][0],0,40);$u['phone_source']='website';$found[]='phone '.$u['phone'];}
    if(!$l['email']&&$c['emails']){$u['email']=mb_substr($c['emails'][0],0,190);$found[]='email '.$u['email'];}
    DB::update('leads',$u,'id=?',[$lid]);
    event($lid,'enriched',$found?'Found on website: '.implode(', ',$found):'Website scanned – no extra contact details found','web');}
  private static function audit($lid,$p){
    $l=DB::row('SELECT l.*,COALESCE(c.keyword,l.category) keyword FROM leads l LEFT JOIN campaigns c ON c.id=l.campaign_id WHERE l.id=?',[$lid]);
    if(!$l)return;
    DB::update('leads',['stage'=>'auditing','last_error'=>null],'id=?',[$lid]);
    self::progress($lid,5,'Starting audit');
    if($l['website']&&!$l['email']){self::progress($lid,8,'Looking for email & phone on the website');self::enrich($lid);}
    $a=jd($l['audit']);
    if($l['website']){$a['website']=Auditor::website($l['website'],fn($pct,$m)=>self::progress($lid,10+$pct*0.4,$m),$l);}
    self::progress($lid,52,'Auditing Google Maps profile');
    $comp=$l['campaign_id']?DB::all('SELECT reviews,rating FROM leads WHERE campaign_id=? AND id<>? ORDER BY position LIMIT 3',[$l['campaign_id'],$lid]):[];
    $a['map']=Auditor::map($l,$comp);
    DB::update('leads',['audit'=>je($a),'web_score'=>$l['website']?$a['website']['score']:null,'map_score'=>$a['map']['score']],'id=?',[$lid]);
    event($lid,'audited',($l['website']?'Website score '.$a['website']['score'].'/100 · ':'No website · ').'Google Maps score '.$a['map']['score'].'/100');
    self::progress($lid,58,'Audit complete – queued for AI report');
    self::push('report',$lid,$p);}
  // Report in three short steps (each its own job, so slow AI calls never run into host time limits):
  // research (skipped when off or cached) → AI writes the report → PDF is rendered
  private static function report($lid,$p){
    $ph=$p['phase']??'research';
    if($ph==='research'){
      DB::update('leads',['stage'=>'reporting'],'id=?',[$lid]);
      self::progress($lid,60,'Preparing audit data for the AI');
      $brief=Report::research($lid,fn($pct,$m)=>self::progress($lid,$pct,$m));
      self::progress($lid,68,'Queued: AI is about to write the report');
      self::push('report',$lid,['phase'=>'write','brief'=>$brief]+$p);return;}
    $bm=$ph==='write'?Batch::mode($p):null;
    if($bm==='batch'){
      self::progress($lid,70,'Waiting to go to the AI together with the other reports (batch)');
      Batch::add($lid,$p,(array)($p['brief']??[]));return;}
    if($bm==='parallel'){
      self::progress($lid,70,'Waiting for a free AI slot ('.Parallel::slots().' reports are written at the same time)');
      Parallel::add($lid,$p,(array)($p['brief']??[]));return;}
    if($ph==='write'||$ph==='save'){
      $rid=$ph==='save'?Batch::save($lid,$p,fn($pct,$m)=>self::progress($lid,$pct,$m)):Report::write($lid,$p,(array)($p['brief']??[]),fn($pct,$m)=>self::progress($lid,$pct,$m));
      if(!$rid)return;
      unset($p['brief'],$p['item']);
      if(report_mode()!=='link'){self::progress($lid,88,'Queued: designing the PDF');self::push('report',$lid,['phase'=>'pdf','rid'=>$rid]+$p);return;}
      self::ready($lid,$p);return;}
    $r=DB::row('SELECT * FROM reports WHERE id=?',[(int)($p['rid']??0)]);
    if($r){self::progress($lid,92,'Rendering PDF pages: charts, illustrations & layout');@ini_set('memory_limit','512M');Report::pdfFile($r);}
    self::ready($lid,$p);}
  static function ready($lid,$p){
    DB::update('leads',['progress'=>100,'progress_msg'=>'Report ready'],'id=?',[$lid]);
    stage($lid,'report_ready','Report generated');
    Automation::fire('report_ready',$lid);
    if(!empty($p['auto']))Outreach::schedule($lid);}}
