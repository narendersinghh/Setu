<?php
// Parallel AI engine for bulk reports – works with every AI provider (Claude, ChatGPT, Gemini, OpenRouter, Groq, DeepSeek, custom).
// One background worker keeps several report requests running at the same time (Settings → Bulk reports → at the same time),
// streams each answer so progress shows live, and saves every report the moment it is finished. If the host stops the
// worker, only the reports still being written are picked up again.
final class Parallel{
  static function slots(){return max(1,min(10,(int)setting('ai_parallel','5')));}
  // Queue the report-writing request of a lead for the engine
  static function add($lid,$p,$brief){
    [$user,$pr]=Report::writeRequest($lid,$p,$brief);
    unset($p['brief']);
    DB::q("UPDATE ai_batch SET status='cancelled' WHERE lead_id=? AND status IN ('queued','sent','running')",[$lid]);
    DB::insert('ai_batch',['mode'=>'parallel','lead_id'=>$lid,'payload'=>je($p+['brief'=>$brief,'pid'=>$pr['id']]),'params'=>je(['system'=>Report::SYSTEM,'user'=>$user,'max'=>16000])]);
    self::kick(date('Y-m-d H:i:s',time()+3));}
  // Make sure a worker runs: a new request starts now even when the worker was waiting for a retry slot
  static function kick($at=null){
    if(DB::val("SELECT id FROM jobs WHERE type='aipar' AND status='pending'")){DB::q("UPDATE jobs SET run_after=LEAST(run_after,?) WHERE type='aipar' AND status='pending'",[$at?:date('Y-m-d H:i:s')]);return;}
    // (a worker that is running right now may already be finishing, so a follow-up run is always queued)
    Queue::push('aipar',0,[],$at);}
  // The streaming request for one report: [url, headers, body]
  private static function request($pv,$model,$key,$a){
    if($pv==='anthropic'){
      $b=['model'=>$model,'max_tokens'=>$a['max'],'system'=>$a['system'],'messages'=>[['role'=>'user','content'=>$a['user']]],'stream'=>true];
      if(preg_match('/^claude-(opus|sonnet|haiku|fable)-5/',$model))$b['output_config']=['effort'=>'medium'];
      return [AI::base().'/v1/messages',['x-api-key: '.$key,'anthropic-version: 2026-10-21'],$b];}
    if($pv==='gemini')return ['https://generativelanguage.googleapis.com/v1beta/models/'.rawurlencode($model).':streamGenerateContent?alt=sse',['x-goog-api-key: '.$key],['systemInstruction'=>['parts'=>[['text'=>$a['system']]]],'contents'=>[['role'=>'user','parts'=>[['text'=>$a['user']]]]],'generationConfig'=>['maxOutputTokens'=>$a['max']]]];
    $base=AI::bases()[$pv]??'';if(!$base)throw new Exception('Unknown AI provider.');
    $b=['model'=>$model,'messages'=>[['role'=>'system','content'=>$a['system']],['role'=>'user','content'=>$a['user']]],'stream'=>true];
    $b[$pv==='openai'?'max_completion_tokens':'max_tokens']=$a['max'];
    if($pv!=='custom')$b['stream_options']=['include_usage'=>true];
    $h=['Authorization: Bearer '.$key];if($pv==='openrouter'){$h[]='HTTP-Referer: '.base_url();$h[]='X-Title: Setu';}
    return [$base.'/chat/completions',$h,$b];}
  // One streamed event (Server-Sent Events) of any provider
  private static function event($pv,$e,$st){
    if(isset($e['error'])){$st->err=is_array($e['error'])?($e['error']['message']??je($e['error'])):(string)$e['error'];return;}
    if($pv==='anthropic'){
      $t=$e['type']??'';
      if($t==='content_block_delta'&&($e['delta']['type']??'')==='text_delta')$st->text.=$e['delta']['text'];
      elseif($t==='message_start'){$u=$e['message']['usage']??[];$st->in=($u['input_tokens']??0)+($u['cache_creation_input_tokens']??0)+($u['cache_read_input_tokens']??0);}
      elseif($t==='message_delta'){$st->out=$e['usage']['output_tokens']??$st->out;$st->stop=$e['delta']['stop_reason']??$st->stop;}
      return;}
    if($pv==='gemini'){
      foreach($e['candidates'][0]['content']['parts']??[] as $pt)$st->text.=$pt['text']??'';
      if(isset($e['usageMetadata'])){$u=$e['usageMetadata'];$st->in=$u['promptTokenCount']??$st->in;$st->out=($u['candidatesTokenCount']??0)+($u['thoughtsTokenCount']??0);}
      return;}
    $st->text.=$e['choices'][0]['delta']['content']??'';
    if(!empty($e['usage'])){$st->in=$e['usage']['prompt_tokens']??$st->in;$st->out=$e['usage']['completion_tokens']??$st->out;}}
  // Worker: keep up to slots() requests running until the queue is empty (stops taking new ones after ~2.5 minutes)
  static function run($jobId=0){
    $pv=setting('ai_provider','anthropic');$model=AI::model($pv);$key=setting('ai_key_'.$pv);
    @set_time_limit(900);@ignore_user_abort(true);
    // Requests left "running" by a worker that was stopped go back into the queue
    DB::q("UPDATE ai_batch SET status='queued',claim=NULL WHERE mode='parallel' AND status='running' AND started_at<NOW()-INTERVAL 7 MINUTE");
    if(!$key||!$model){foreach(DB::all("SELECT * FROM ai_batch WHERE mode='parallel' AND status='queued'") as $it)self::giveUp($it,'No API key or model set in AI Connection.');return;}
    $mh=curl_multi_init();$act=[];$t0=time();$budget=150;$beat=0;$n=self::slots();
    $start=function()use(&$act,$mh,$pv,$model,$key){
      $c=token(8);
      if(!DB::q("UPDATE ai_batch SET status='running',claim=?,started_at=NOW(),attempts=attempts+1 WHERE mode='parallel' AND status='queued' AND (next_at IS NULL OR next_at<=NOW()) ORDER BY id LIMIT 1",[$c])->rowCount())return false;
      $it=DB::row('SELECT * FROM ai_batch WHERE claim=?',[$c]);
      [$url,$h,$body]=self::request($pv,$model,$key,jd($it['params']));
      $st=(object)['buf'=>'','text'=>'','in'=>0,'out'=>0,'err'=>'','raw'=>'','stop'=>'','t0'=>microtime(true)];
      $ch=curl_init($url);
      curl_setopt_array($ch,[CURLOPT_POST=>true,CURLOPT_POSTFIELDS=>je($body),CURLOPT_HTTPHEADER=>array_merge($h,['Content-Type: application/json','Accept: text/event-stream']),CURLOPT_TIMEOUT=>300,CURLOPT_CONNECTTIMEOUT=>15,CURLOPT_PROTOCOLS=>CURLPROTO_HTTP|CURLPROTO_HTTPS,
        CURLOPT_WRITEFUNCTION=>function($c,$d)use($st,$pv){
          if(strlen($st->raw)<3000)$st->raw.=$d;
          $st->buf.=$d;
          while(($i=strpos($st->buf,"\n"))!==false){$ln=rtrim(substr($st->buf,0,$i),"\r");$st->buf=substr($st->buf,$i+1);if(strncmp($ln,'data:',5))continue;$j=trim(substr($ln,5));if($j===''||$j==='[DONE]')continue;$e=json_decode($j,true);if(is_array($e))self::event($pv,$e,$st);}
          return strlen($d);}]);
      curl_multi_add_handle($mh,$ch);
      $sid=DB::insert('job_steps',['lead_id'=>$it['lead_id'],'kind'=>'ai','step'=>'Writing the report (parallel engine'.((int)$it['attempts']>1?', try '.(int)$it['attempts']:'').')','status'=>'run','model'=>mb_substr($model,0,90),'detail'=>'Sent to the AI – waiting for the first words']);
      DB::update('ai_batch',['step_id'=>$sid],'id=?',[$it['id']]);
      DB::update('leads',['progress'=>72,'progress_msg'=>'AI is writing the report'],'id=?',[$it['lead_id']]);
      $act[(int)$ch]=[$ch,$it,$st,$sid];return true;};
    $fill=function()use(&$act,$start,$n,$t0,$budget){while(count($act)<$n&&time()-$t0<$budget&&$start());};
    $fill();
    while($act){
      curl_multi_exec($mh,$running);curl_multi_select($mh,1);
      while($info=curl_multi_info_read($mh)){
        $k=(int)$info['handle'];if(!isset($act[$k]))continue;[$ch,$it,$st,$sid]=$act[$k];unset($act[$k]);
        $code=(int)curl_getinfo($ch,CURLINFO_HTTP_CODE);$cerr=$info['result']?curl_strerror($info['result']):'';
        curl_multi_remove_handle($mh,$ch);curl_close($ch);
        self::finish($it,$st,$sid,$code,$cerr,$pv,$model);
        $fill();}
      // Every 4 seconds: live progress, keep the job alive, stop requests the user aborted
      if(microtime(true)-$beat>=4){$beat=microtime(true);
        foreach($act as $k=>[$ch,$it,$st,$sid]){
          if(DB::val('SELECT status FROM ai_batch WHERE id=?',[$it['id']])==='cancelled'){curl_multi_remove_handle($mh,$ch);curl_close($ch);unset($act[$k]);DB::update('job_steps',['status'=>'err','detail'=>'Stopped by user','ms'=>(int)((microtime(true)-$st->t0)*1000)],'id=?',[$sid]);continue;}
          $w=(int)round(mb_strlen($st->text)/6);$s=(int)(microtime(true)-$st->t0);$el=$s<60?$s.'s':floor($s/60).'m '.($s%60).'s';
          DB::update('leads',['progress'=>min(84,72+(int)($w/90)),'progress_msg'=>'AI is writing the report · '.number_format($w).' words · '.$el],'id=?',[$it['lead_id']]);
          DB::update('job_steps',['detail'=>($w?number_format($w).' words written so far':'Waiting for the first words').' · '.$el],'id=?',[$sid]);}
        if($jobId)DB::q("UPDATE jobs SET locked_at=NOW() WHERE id=? AND status='running'",[$jobId]);
        $fill();}}
    curl_multi_close($mh);
    // More waiting (or waiting for a retry slot): run again
    if(($nx=DB::val("SELECT MIN(COALESCE(next_at,NOW())) FROM ai_batch WHERE mode='parallel' AND status='queued'"))&&!DB::val("SELECT id FROM jobs WHERE type='aipar' AND status='pending'"))Queue::push('aipar',0,[],max(date('Y-m-d H:i:s'),$nx));}
  private static function finish($it,$st,$sid,$code,$cerr,$pv,$model){
    $ms=(int)((microtime(true)-$st->t0)*1000);
    $ok=$code===200&&$cerr===''&&$st->err===''&&trim($st->text)!==''&&$st->stop!=='refusal';
    $cost=Usage::log($pv,'report',1,$st->in,$st->out,$model,!$ok);
    if($ok){
      DB::update('ai_batch',['status'=>'done','result'=>$st->text,'tokens_in'=>$st->in,'tokens_out'=>$st->out],'id=?',[$it['id']]);
      DB::update('job_steps',['status'=>'ok','ms'=>$ms,'tokens_in'=>$st->in,'tokens_out'=>$st->out,'cost'=>round($cost,5),'detail'=>'Written in '.round($ms/1000).'s · '.number_format(mb_strlen($st->text)).' characters'],'id=?',[$sid]);
      DB::update('leads',['progress'=>85,'progress_msg'=>'Report written – saving'],'id=?',[$it['lead_id']]);
      Queue::push('report',$it['lead_id'],['phase'=>'save','item'=>(int)$it['id']]+array_diff_key(jd($it['payload']),['brief'=>1]));return;}
    // Why it failed, in plain words
    $why=$st->err?:($cerr?:($st->stop==='refusal'?'The AI declined this request':($code>=400?'HTTP '.$code.': '.self::msg($st->raw):(trim($st->text)===''?'The AI returned an empty answer':'HTTP '.$code))));
    $retry=(int)$it['attempts']<3&&($code===0||$code===408||$code===429||$code>=500||$cerr!==''||$st->err!==''&&preg_match('/overload|rate|timeout|busy|capacity/i',$st->err));
    if($retry){$wait=$code===429?60*(int)$it['attempts']:20*(int)$it['attempts'];DB::q("UPDATE ai_batch SET status='queued',claim=NULL,next_at=NOW()+INTERVAL ? SECOND WHERE id=? AND status='running'",[$wait,$it['id']]);DB::update('job_steps',['status'=>'err','ms'=>$ms,'detail'=>mb_substr($why,0,400).' – trying again in '.$wait.'s'],'id=?',[$sid]);DB::update('leads',['progress_msg'=>'AI busy – trying again in '.$wait.'s'],'id=?',[$it['lead_id']]);return;}
    DB::update('job_steps',['status'=>'err','ms'=>$ms,'detail'=>mb_substr($why,0,400)],'id=?',[$sid]);
    self::giveUp($it,$why);}
  private static function msg($raw){$d=json_decode($raw,true);foreach([$d['error']['message']??null,$d['error']??null,$d['message']??null,$d[0]['error']['message']??null] as $v)if(is_string($v)&&$v!=='')return $v;return mb_substr(strip_tags($raw),0,200);}
  // Not possible in parallel: write this report on its own (that step reports the exact error if it fails too)
  private static function giveUp($it,$why){
    DB::update('ai_batch',['status'=>'error','error'=>mb_substr($why,0,500)],'id=?',[$it['id']]);
    Queue::push('report',$it['lead_id'],['phase'=>'write','nobatch'=>1]+jd($it['payload']));}}
