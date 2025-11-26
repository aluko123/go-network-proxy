#!/usr/bin/env python3
"""
Mock inference worker for integration testing.
Echoes back tokens without loading a real model - fast and lightweight.
"""
import argparse
import asyncio
import logging
import grpc
import inference_pb2
import inference_pb2_grpc

logging.basicConfig(level=logging.INFO, format='%(asctime)s [%(levelname)s] %(message)s')
logger = logging.getLogger(__name__)


class MockModelService(inference_pb2_grpc.ModelServiceServicer):
    def __init__(self, model_name: str, latency: float = 0.0):
        self.model_name = model_name
        self.latency = latency
        logger.info(f"Mock worker initialized: model={model_name}, latency={latency}s")

    async def Generate(self, request, context):
        request_id = request.request_id or "unknown"
        logger.info(f"[{self.model_name}] Received request {request_id}: prompt='{request.prompt[:50]}...'")

        # Mock response: split prompt into words and echo them as tokens
        words = request.prompt.split()
        max_tokens = min(request.max_tokens, len(words) + 5)

        # Generate mock tokens
        mock_tokens = [f"[{self.model_name}]"] + words[:max_tokens-1]

        for i, token in enumerate(mock_tokens):
            if self.latency > 0:
                await asyncio.sleep(self.latency)
            
            yield inference_pb2.TokenResponse(
                request_id=request_id,
                token=token + " ",
                token_count=i + 1,
                finished=False
            )

        # Final message
        yield inference_pb2.TokenResponse(
            request_id=request_id,
            token="",
            token_count=len(mock_tokens),
            finished=True
        )
        logger.info(f"[{self.model_name}] Finished request {request_id}")

    async def Health(self, request, context):
        return inference_pb2.HealthResponse(
            healthy=True,
            current_queue_size=0,
            gpu_utilization=0.0
        )


async def serve(args):
    service = MockModelService(args.model, args.latency)
    
    server = grpc.aio.server()
    inference_pb2_grpc.add_ModelServiceServicer_to_server(service, server)
    
    listen_addr = f'[::]:{args.port}'
    server.add_insecure_port(listen_addr)
    logger.info(f"Starting Mock gRPC Worker on {listen_addr}")
    
    await server.start()
    await server.wait_for_termination()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description="Mock inference worker for testing")
    parser.add_argument("--model", type=str, default="mock-gpt", help="Model name to report")
    parser.add_argument("--port", type=int, default=50051, help="Port to listen on")
    parser.add_argument("--latency", type=float, default=0.01, help="Artificial latency per token (seconds)")
    args = parser.parse_args()

    asyncio.run(serve(args))
