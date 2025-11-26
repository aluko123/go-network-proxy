import argparse
import asyncio
import logging
import time
import threading
import grpc
import torch
from transformers import AutoModelForCausalLM, AutoTokenizer, TextIteratorStreamer
import inference_pb2
import inference_pb2_grpc

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s [%(levelname)s] %(message)s')
logger = logging.getLogger(__name__)

class ModelService(inference_pb2_grpc.ModelServiceServicer):
    def __init__(self, model_name, device="cpu", latency=0.0):
        logger.info(f"Loading model {model_name} on {device}...")
        self.device = device
        self.latency = latency
        self.tokenizer = AutoTokenizer.from_pretrained(model_name)
        self.model = AutoModelForCausalLM.from_pretrained(model_name).to(device)
        logger.info("Model loaded successfully!")

    async def Generate(self, request, context):
        request_id = request.request_id or "unknown"
        logger.info(f"Received request {request_id}: prompt='{request.prompt}'")

        # 1. Tokenize
        inputs = self.tokenizer(request.prompt, return_tensors="pt").to(self.device)
        
        # 2. Setup Streaming
        streamer = TextIteratorStreamer(self.tokenizer, skip_prompt=True, skip_special_tokens=True)
        
        # Handle temperature=0 case (Greedy Decoding)
        do_sample = True
        if request.temperature < 1e-5:
             do_sample = False

        generation_kwargs = dict(
            **inputs,
            streamer=streamer,
            max_new_tokens=request.max_tokens,
            do_sample=do_sample,
        )
        if do_sample:
            generation_kwargs["temperature"] = request.temperature

        # 3. Run Generation in a separate thread (since model.generate is blocking)
        thread = threading.Thread(target=self.model.generate, kwargs=generation_kwargs)
        thread.start()

        # 4. Yield Tokens
        try:
            for new_text in streamer:
                # Simulate latency if configured
                if self.latency > 0:
                    await asyncio.sleep(self.latency)
                
                yield inference_pb2.TokenResponse(
                    request_id=request_id,
                    token=new_text,
                    finished=False
                )
            
            # Final message
            yield inference_pb2.TokenResponse(
                request_id=request_id,
                token="",
                finished=True
            )
            logger.info(f"Finished request {request_id}")

        except Exception as e:
            logger.error(f"Error generating for {request_id}: {e}")
            yield inference_pb2.TokenResponse(
                request_id=request_id,
                error=str(e),
                finished=True
            )
        
        thread.join()

    async def Health(self, request, context):
        return inference_pb2.HealthResponse(
            healthy=True,
            current_queue_size=0,
            gpu_utilization=0.0 # CPU mode
        )

async def serve(args):
    service = ModelService(args.model, args.device, args.latency)
    
    server = grpc.aio.server()
    inference_pb2_grpc.add_ModelServiceServicer_to_server(
        service, server
    )
    listen_addr = f'[::]:{args.port}'
    server.add_insecure_port(listen_addr)
    logger.info(f"Starting gRPC Worker on {listen_addr} (latency={args.latency}s)")
    
    await server.start()
    await server.wait_for_termination()

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", type=str, default="gpt2")
    parser.add_argument("--port", type=int, default=50051)
    parser.add_argument("--device", type=str, default="cpu")
    parser.add_argument("--latency", type=float, default=0.0, help="Artificial latency per token in seconds")
    args = parser.parse_args()

    asyncio.run(serve(args))
