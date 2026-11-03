<?php
final class AI{
  static function providers(){return [
    'anthropic'=>['name'=>'Anthropic Claude','url'=>'https://console.anthropic.com/settings/keys','models'=>'claude-opus-5-5, claude-sonnet-5-5, claude-haiku-5-5','steps'=>['Go to console.anthropic.com and sign in (or create an account).','Open Settings → Billing and add credits.','Open Settings → API Keys → "Create Key". Copy the key (starts with sk-ant-).','Paste it below, choose a model and click Save, then "Test connection".'],'note'=>'Recommended. Best report writing quality. claude-opus-5-5 = best quality, claude-haiku-5-5 = cheapest.'],
    'openai'=>['name'=>'OpenAI ChatGPT','url'=>'https://platform.openai.com/api-keys','models'=>'gpt-4.1-mini, gpt-4.1, gpt-4o','steps'=>['Go to platform.openai.com and sign in.','Open Settings → Billing and add credits (ChatGPT Plus does NOT include API usage).','Open API keys → "Create new secret key". Copy it (starts with sk-).','Paste it below with a model name, Save and Test.'],'note'=>'Use any chat model available on your OpenAI account.'],
    'openrouter'=>['name'=>'OpenRouter','url'=>'https://openrouter.ai/keys','models'=>'openrouter/auto, anthropic/…, openai/…, google/…','steps'=>['Go to openrouter.ai and sign in.','Add credits under Credits.','Open Keys → "Create Key" and copy it (starts with sk-or-).','Paste below. Model ids look like "anthropic/claude-…" – see openrouter.ai/models.'],'note'=>'One key, hundreds of models (Claude, GPT, Gemini, Llama…).'],
    'gemini'=>['name'=>'Google Gemini','url'=>'https://aistudio.google.com/app/apikey','models'=>'gemini-2.5-flash, gemini-2.5-pro','steps'=>['Go to aistudio.google.com and sign in with Google.','Click "Get API key" → "Create API key".','Copy the key and paste it below with a model name.'],'note'=>'Has a free tier with rate limits.'],
    'groq'=>['name'=>'Groq','url'=>'https://console.groq.com/keys','models'=>'llama-3.3-70b-versatile','steps'=>['Go to console.groq.com and sign in.','Open API Keys → Create API Key.','Paste it below with a model name.'],'note'=>'Very fast open-source models.'],
    'deepseek'=>['name'=>'DeepSeek','url'=>'https://platform.deepseek.com/api_keys','models'=>'deepseek-chat','steps'=>['Go to platform.deepseek.com and sign in.','Top up balance, then create an API key.','Paste it below.'],'note'=>'Low-cost option.'],
    'custom'=>['name'=>'Custom (OpenAI-compatible)','url'=>'','models'=>'any','steps'=>['Use any provider with an OpenAI-compatible /chat/completions endpoint (Together, Mistral, Azure OpenAI proxy, self-hosted…).','Enter the full base URL, e.g. https://api.together.xyz/v1','Enter the key and model name.'],'note'=>'For advanced users.']];}
  // API address (can point to a proxy/gateway in AI Connection → advanced)
  // OpenAI-compatible chat endpoints
  static function bases(){return ['openai'=>'https://api.openai.com/v1','openrouter'=>'https://openrouter.ai/api/v1','groq'=>'https://api.groq.com/openai/v1','deepseek'=>'https://api.deepseek.com/v1','custom'=>rtrim(setting('ai_custom_url'),'/')];}
  static function base(){return rtrim(setting('anthropic_base','https://api.anthropic.com'),'/');}
  static function model($p=null){$p=$p?:setting('ai_provider','anthropic');return setting('ai_model_'.$p);}
  // Tokens, cost and web searches of the current call (shown in "View process")
  static $st=[];
  private static function count($cost,$in,$out,$ws=0){self::$st['in']+=$in;self::$st['out']+=$out;self::$st['cost']+=$cost;self::$st['ws']+=$ws;self::$st['turns']++;}
  static function complete($system,$user,$max=12000,$p=null,$o=[]){
    $p=$p?:setting('ai_provider','anthropic');
    $key=setting('ai_key_'.$p);$model=self::model($p);
    if(!$key)throw new Exception('No API key for '.(self::providers()[$p]['name']??$p).'. Add it in the AI Connection tab.');
    if(!$model)throw new Exception('No AI model selected in the AI Connection tab.');
    @set_time_limit(max(300,(int)($o['deadline']??280)+60));
    $op=$o['op']??'chat';$label=$o['label']??ucfirst($op);
    self::$st=['in'=>0,'out'=>0,'cost'=>0.0,'ws'=>0,'turns'=>0];
    Trace::step($label,'ai',['model'=>mb_substr($model,0,90)]);
    $o['tick']=Trace::tick($label.' – waiting for '.$model);
    $t0=microtime(true);
    try{
      if($p==='anthropic')$t=self::anthropic($key,$model,$system,$user,$max,$o);
      elseif($p==='gemini')$t=self::gemini($key,$model,$system,$user,$max,$op,min(240,(int)($o['deadline']??240)),$o['tick']);
      else $t=self::openai($p,$key,$model,$system,$user,$max,$o);
      $s=self::$st;
      Trace::close('ok','Answered in '.round(microtime(true)-$t0,1).'s · '.number_format(mb_strlen($t)).' characters'.($s['ws']?' · '.$s['ws'].' web search'.($s['ws']>1?'es':''):'').($s['turns']>1?' · '.$s['turns'].' requests':''),['tokens_in'=>$s['in'],'tokens_out'=>$s['out'],'cost'=>round($s['cost'],5)]);
      return $t;}
    catch(Throwable $e){$s=self::$st;Trace::close('err',$e->getMessage(),['tokens_in'=>$s['in'],'tokens_out'=>$s['out'],'cost'=>round($s['cost'],5)]);throw $e;}}
  private static function openai($p,$key,$model,$system,$user,$max,$o){
    $op=$o['op']??'chat';
    $base=self::bases()[$p]??'';
    if(!$base)throw new Exception('Unknown AI provider.');
    $body=['model'=>$model,'messages'=>[['role'=>'system','content'=>$system],['role'=>'user','content'=>$user]]];
    $body[$p==='openai'?'max_completion_tokens':'max_tokens']=$max;
    $h=['Authorization: Bearer '.$key];
    if($p==='openrouter'){$h[]='HTTP-Referer: '.base_url();$h[]='X-Title: Setu';}
    $r=self::retry(fn()=>http_json($base.'/chat/completions',['json'=>$body,'headers'=>$h,'timeout'=>min(240,(int)($o['deadline']??240)),'tick'=>$o['tick']??null]),time()+(int)($o['deadline']??240));
    if(!empty($r['aborted']))throw new Aborted('Stopped by user');
    $in=$r['data']['usage']['prompt_tokens']??0;$out=$r['data']['usage']['completion_tokens']??0;
    self::count(Usage::log($p,$op,1,$in,$out,$model,$r['code']!==200),$in,$out);
    if($r['code']===0)throw new Exception('The AI did not answer in time ('.($r['error']?:'timeout').'). Try again or choose a faster model in AI Connection.');
    if($r['code']!==200)throw new Exception('AI error '.$r['code'].': '.($r['data']['error']['message']??$r['error']?:substr($r['body'],0,300)));
    $t=$r['data']['choices'][0]['message']['content']??'';
    if($t==='')throw new Exception('AI returned an empty response.');
    return $t;}
  // Busy / rate-limited provider: wait a little and try once more (only when there is time left)
  private static function retry($f,$until){
    $r=$f();
    if(in_array($r['code'],[429,500,502,503,529],true)&&$until-time()>90){Trace::note('The AI provider is busy (HTTP '.$r['code'].') – retrying once');sleep(min(15,max(3,(int)($r['data']['retry_after']??6))));$r=$f();}
    return $r;}
  private static function anthropic($key,$model,$system,$user,$max,$o=[]){
    $msgs=[['role'=>'user','content'=>$user]];
    $body=['model'=>$model,'max_tokens'=>$max,'system'=>$system];
    $h=['x-api-key: '.$key,'anthropic-version: 2026-10-21'];
    if(preg_match('/^claude-(opus|sonnet|haiku|fable)-5/',$model))$body['output_config']=['effort'=>'medium'];
    // Optional live web research (industry benchmarks, search demand) with Anthropic's server-side web search
    if(!empty($o['research']))$body['tools']=[['type'=>preg_match('/^claude-(opus|sonnet|fable)-|^claude-sonnet-4-6|^claude-opus-4-[678]/',$model)?'web_search_20260209':'web_search_20250305','name'=>'web_search','max_uses'=>(int)($o['searches']??4)]];
    // Server-side refusal fallback where supported
    $fb=preg_match('/^claude-(opus-5|opus-5-5|sonnet-5-5|fable-5-1)$/',$model);
    $t='';$tin=0;$tout=0;$ws=0;
    // Stay inside one background step: every request gets only the time that is left
    $t0=time();$dl=(int)($o['deadline']??280);$left=fn()=>max(45,min(280,$dl-(time()-$t0)));
    try{
    for($turn=0;$turn<4;$turn++){
      if($turn&&time()-$t0>$dl-45)break;
      $req=$body+['messages'=>$msgs];
      $r=self::retry(fn()=>http_json(self::base().'/v1/messages',['json'=>$fb?$req+['fallbacks'=>'default']:$req,'headers'=>$fb?array_merge($h,['anthropic-beta: server-side-fallback-2026-07-01']):$h,'timeout'=>$left(),'tick'=>$o['tick']??null]),$t0+$dl);
      if(!empty($r['aborted']))throw new Aborted('Stopped by user');
      if($r['code']===400&&$fb)$r=http_json(self::base().'/v1/messages',['json'=>$req,'headers'=>$h,'timeout'=>$left(),'tick'=>$o['tick']??null]);
      if($r['code']===400&&isset($body['tools'])){unset($body['tools']);Trace::note('Web search is not available for this model – continuing without it');$r=http_json(self::base().'/v1/messages',['json'=>$body+['messages'=>$msgs],'headers'=>$h,'timeout'=>$left(),'tick'=>$o['tick']??null]);}
      if(!empty($r['aborted']))throw new Aborted('Stopped by user');
      $u=$r['data']['usage']??[];$tin+=($u['input_tokens']??0)+($u['cache_creation_input_tokens']??0)+($u['cache_read_input_tokens']??0);$tout+=$u['output_tokens']??0;$ws+=$u['server_tool_use']['web_search_requests']??0;
      if($r['code']===0)throw new Exception('Claude did not answer in time ('.($r['error']?:'timeout').'). Try again, or choose a faster model (e.g. claude-sonnet-5-5) in AI Connection.');
      if($r['code']!==200)throw new Exception('Claude API error '.$r['code'].': '.($r['data']['error']['message']??$r['error']?:substr($r['body'],0,300)));
      $stop=$r['data']['stop_reason']??'';
      if($stop==='refusal')throw new Exception('Claude declined this request ('.($r['data']['stop_details']['category']??'policy').'). Try again or edit the prompt.');
      foreach($r['data']['content']??[] as $b)if(($b['type']??'')==='text')$t.=$b['text'];
      if($stop!=='pause_turn')break;
      $msgs[]=['role'=>'assistant','content'=>$r['data']['content']];}}
    finally{$c=Usage::log('anthropic',$o['op']??'chat',1,$tin,$tout,$model,trim($t)==='');if($ws)$c+=Usage::log('web_search',$o['op']??'chat',$ws);self::count($c,$tin,$tout,$ws);self::$st['turns']=max(1,$turn+1);}
    if(trim($t)==='')throw new Exception('Claude returned an empty response.');
    return $t;}
  private static function gemini($key,$model,$system,$user,$max,$op='chat',$to=240,$tick=null){
    $r=http_json('https://generativelanguage.googleapis.com/v1beta/models/'.rawurlencode($model).':generateContent',['headers'=>['x-goog-api-key: '.$key],'json'=>['systemInstruction'=>['parts'=>[['text'=>$system]]],'contents'=>[['role'=>'user','parts'=>[['text'=>$user]]]],'generationConfig'=>['maxOutputTokens'=>$max]],'timeout'=>$to,'tick'=>$tick]);
    if(!empty($r['aborted']))throw new Aborted('Stopped by user');
    $in=$r['data']['usageMetadata']['promptTokenCount']??0;$out=($r['data']['usageMetadata']['candidatesTokenCount']??0)+($r['data']['usageMetadata']['thoughtsTokenCount']??0);
    self::count(Usage::log('gemini',$op,1,$in,$out,$model,$r['code']!==200),$in,$out);
    if($r['code']!==200)throw new Exception('Gemini error '.$r['code'].': '.($r['data']['error']['message']??$r['error']?:substr($r['body'],0,300)));
    $t='';foreach($r['data']['candidates'][0]['content']['parts']??[] as $pt)$t.=$pt['text']??'';
    if($t==='')throw new Exception('Gemini returned an empty response.');
    return $t;}}
