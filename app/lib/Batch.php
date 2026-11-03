<?php
// Bulk reports through Anthropic's Message Batches API: all report-writing requests of a bulk run go to Claude in one
// batch (50% cheaper). Nothing waits on the AI inside a web request – a short job submits the batch, checks it every
// 20 seconds and, when it has ended, hands each answer to its own short "save" step.
final class Batch{
  // Use a batch for this report? (Claude only; Settings → Prospecting → Report delivery → Bulk reports)
  static function on($p){$m=setting('ai_batch','auto');return setting('ai_provider','anthropic')==='anthropic'&&$m!=='off'&&empty($p['nobatch'])&&($m==='on'||!empty($p['bulk']));}
  private static function h(){return ['x-api-key: '.setting('ai_key_anthropic'),'anthropic-version: 2026-10-21'];}
  // Queue the report-writing request of a lead for the next batch
  static function add($lid,$p,$brief){
    [$user,$pr]=Report::writeRequest($lid,$p,$brief);$model=AI::model('anthropic');
    $params=['model'=>$model,'max_tokens'=>16000,'system'=>Report::SYSTEM,'messages'=>[['role'=>'user','content'=>$user]]];
    if(preg_match('/^claude-(opus|sonnet|haiku|fable)-5/',$model))$params['output_config']=['effort'=>'medium'];
    unset($p['brief']);
    DB::q("UPDATE ai_batch SET status='cancelled' WHERE lead_id=? AND status IN ('queued','sent')",[$lid]);
    DB::insert('ai_batch',['lead_id'=>$lid,'payload'=>je($p+['brief'=>$brief,'pid'=>$pr['id']]),'params'=>je($params)]);
    // Collect the whole bulk run for a few seconds, then send it as one batch
    if(!DB::val("SELECT id FROM jobs WHERE type='batch' AND status IN ('pending','running')"))Queue::push('batch',0,[],date('Y-m-d H:i:s',time()+15));}
  private static function step($lids,$step,$st='ok',$detail=null,$meta=[]){foreach((array)$lids as $l)DB::insert('job_steps',['lead_id'=>$l,'kind'=>$meta?'ai':'step','step'=>mb_substr($step,0,190),'status'=>$st,'detail'=>$detail]+$meta);}
  // Back to the normal one-by-one path (batch failed, expired or answer unusable)
  private static function fallback($items,$why){
    foreach($items as $it){
      $p=jd($it['payload']);DB::update('ai_batch',['status'=>'error','error'=>mb_substr($why,0,500)],'id=?',[$it['id']]);
      self::step($it['lead_id'],'AI batch did not work – writing this report directly instead','err',$why);
      Queue::push('report',$it['lead_id'],['phase'=>'write','nobatch'=>1]+$p);}}
  // The batch job: send waiting requests, check sent batches, store finished answers
  static function tick(){
    try{self::run();}
    finally{if(DB::val("SELECT id FROM ai_batch WHERE status IN ('queued','sent') LIMIT 1")&&!DB::val("SELECT id FROM jobs WHERE type='batch' AND status='pending'"))Queue::push('batch',0,[],date('Y-m-d H:i:s',time()+20));}}
  private static function run(){
    // 1 · send everything that is waiting as one batch
    $q=DB::all("SELECT * FROM ai_batch WHERE status='queued' ORDER BY id LIMIT 500");
    // wait a little for the rest of a bulk run, unless the queue has stopped growing
    if($q&&!DB::val("SELECT id FROM ai_batch WHERE status='queued' AND created_at>NOW()-INTERVAL 10 SECOND")){
      $req=[];foreach($q as $it)$req[]=['custom_id'=>'r'.$it['id'],'params'=>jd($it['params'])];
      $r=http_json(AI::base().'/v1/messages/batches',['json'=>['requests'=>$req],'headers'=>self::h(),'timeout'=>60]);
      $ids=array_column($q,'id');[$in,$v]=DB::in($ids);
      if($r['code']===200&&!empty($r['data']['id'])){
        DB::q("UPDATE ai_batch SET status='sent',batch_id=?,sent_at=NOW() WHERE id IN ($in) AND status='queued'",array_merge([$r['data']['id']],$v));
        $lids=array_column($q,'lead_id');[$li,$lv]=DB::in($lids);
        DB::q("UPDATE leads SET progress=72,progress_msg=? WHERE id IN ($li)",array_merge(['AI batch: '.count($q).' reports are being written together · usually a few minutes'],$lv));
        self::step($lids,'Sent to the AI as one batch with '.count($q).' report'.(count($q)>1?'s':'').' (50% cheaper, no waiting on the server)','ok','Batch '.$r['data']['id']);}
      else self::fallback($q,'Batch could not be sent (HTTP '.$r['code'].'): '.($r['data']['error']['message']??$r['error']?:mb_substr($r['body'],0,200)));}
    // 2 · check sent batches
    foreach(DB::all("SELECT batch_id,MIN(sent_at) sent,COUNT(*) n FROM ai_batch WHERE status='sent' GROUP BY batch_id") as $b){
      $r=http_json(AI::base().'/v1/messages/batches'.'/'.rawurlencode($b['batch_id']),['headers'=>self::h(),'timeout'=>30]);
      if($r['code']!==200)continue;
      $d=$r['data'];$c=$d['request_counts']??[];$done=(int)($c['succeeded']??0)+(int)($c['errored']??0)+(int)($c['expired']??0)+(int)($c['canceled']??0);$age=time()-strtotime($b['sent']);
      $items=DB::all("SELECT * FROM ai_batch WHERE batch_id=? AND status='sent'",[$b['batch_id']]);
      if(($d['processing_status']??'')!=='ended'){
        // Taking far too long: cancel it and write these reports one by one instead
        if($age>2700){http_json(AI::base().'/v1/messages/batches'.'/'.rawurlencode($b['batch_id']).'/cancel',['method'=>'POST','body'=>'','headers'=>self::h(),'timeout'=>20]);self::fallback($items,'The AI batch took longer than 45 minutes');continue;}
        [$li,$lv]=DB::in(array_column($items,'lead_id'));
        DB::q("UPDATE leads SET progress_msg=? WHERE id IN ($li) AND stage='reporting'",array_merge(['AI batch: '.$done.' of '.(int)$b['n'].' reports written · '.($age<60?$age.'s':floor($age/60).'m '.($age%60).'s')],$lv));
        continue;}
      // 3 · ended: store each answer and give it its own short save step
      $res=http($d['results_url'],['headers'=>self::h(),'timeout'=>120]);
      if($res['code']!==200){if($age>2700)self::fallback($items,'Batch results could not be downloaded (HTTP '.$res['code'].')');continue;}
      $by=[];foreach(preg_split('/\r?\n/',trim($res['body'])) as $ln){$x=json_decode($ln,true);if(isset($x['custom_id']))$by[$x['custom_id']]=$x['result']??[];}
      $model=AI::model('anthropic');
      foreach($items as $it){
        $x=$by['r'.$it['id']]??null;
        if(($x['type']??'')!=='succeeded'){self::fallback([$it],'Batch result: '.($x['type']??'missing').(isset($x['error']['error']['message'])?' – '.$x['error']['error']['message']:''));continue;}
        $m=$x['message'];$t='';foreach($m['content']??[] as $bl)if(($bl['type']??'')==='text')$t.=$bl['text'];
        $u=$m['usage']??[];$tin=($u['input_tokens']??0)+($u['cache_creation_input_tokens']??0)+($u['cache_read_input_tokens']??0);$tout=$u['output_tokens']??0;
        $cost=Usage::log('anthropic','report_batch',1,$tin,$tout,$model,trim($t)==='',0.5);
        DB::update('ai_batch',['status'=>'done','result'=>$t,'tokens_in'=>$tin,'tokens_out'=>$tout],'id=?',[$it['id']]);
        self::step($it['lead_id'],'Writing the report (AI batch)','ok','Written in the batch · '.number_format(mb_strlen($t)).' characters · half price',['model'=>mb_substr($model,0,90),'tokens_in'=>$tin,'tokens_out'=>$tout,'cost'=>round($cost,5),'ms'=>$age*1000]);
        Queue::push('report',$it['lead_id'],['phase'=>'save','item'=>(int)$it['id']]+array_diff_key(jd($it['payload']),['brief'=>1]));}}}
  // Save step for one finished batch answer
  static function save($lid,$p,$progress){
    $it=DB::row("SELECT * FROM ai_batch WHERE id=? AND lead_id=? AND status='done'",[(int)($p['item']??0),$lid]);
    if(!$it)throw new Exception('The batch answer for this report was not found – start the report again.');
    $pl=jd($it['payload']);
    $rid=Report::finish($lid,(string)$it['result'],(array)($pl['brief']??[]),(int)($pl['pid']??0),$progress);
    DB::update('ai_batch',['status'=>'saved','params'=>null,'result'=>null],'id=?',[$it['id']]);
    if(!$rid){Trace::step('The batch answer was not valid JSON – writing this report again directly','step');Trace::close('err');Queue::push('report',$lid,['phase'=>'write','nobatch'=>1]+$pl);return null;}
    return $rid;}}
