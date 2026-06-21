FROM fnproject/python:3.11-dev AS build-stage

WORKDIR /function
ENV PYTHONPATH=/python

COPY requirements.txt /function/
RUN pip3 install --target /python -r requirements.txt

COPY func.py /function/

FROM fnproject/python:3.11

WORKDIR /function
COPY --from=build-stage /python /python
COPY --from=build-stage /function /function

ENV PYTHONPATH=/python
ENTRYPOINT ["/python/bin/fdk", "/function/func.py", "handler"]
